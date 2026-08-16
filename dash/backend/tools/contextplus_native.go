package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danilrybalkin/apollo-dash/db"
	sitter "github.com/smacker/go-tree-sitter"
	sitterbash "github.com/smacker/go-tree-sitter/bash"
	sitterc "github.com/smacker/go-tree-sitter/c"
	sittercpp "github.com/smacker/go-tree-sitter/cpp"
	sittercsharp "github.com/smacker/go-tree-sitter/csharp"
	sittergolang "github.com/smacker/go-tree-sitter/golang"
	sitterjava "github.com/smacker/go-tree-sitter/java"
	sitterjavascript "github.com/smacker/go-tree-sitter/javascript"
	sitterkotlin "github.com/smacker/go-tree-sitter/kotlin"
	sitterphp "github.com/smacker/go-tree-sitter/php"
	sitterpython "github.com/smacker/go-tree-sitter/python"
	sitterruby "github.com/smacker/go-tree-sitter/ruby"
	sitterrust "github.com/smacker/go-tree-sitter/rust"
	sitterscala "github.com/smacker/go-tree-sitter/scala"
	sittersql "github.com/smacker/go-tree-sitter/sql"
	sitterswift "github.com/smacker/go-tree-sitter/swift"
	sittertsx "github.com/smacker/go-tree-sitter/typescript/tsx"
	sittertypescript "github.com/smacker/go-tree-sitter/typescript/typescript"
	sitteryaml "github.com/smacker/go-tree-sitter/yaml"
	"gonum.org/v1/gonum/mat"
)

var contextCodeExt = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".py": true, ".rs": true, ".java": true, ".kt": true, ".swift": true,
	".c": true, ".h": true, ".cpp": true, ".hpp": true, ".cs": true,
	".php": true, ".rb": true, ".scala": true, ".m": true, ".mm": true,
	".sql": true, ".sh": true, ".yaml": true, ".yml": true, ".json": true,
	".toml": true,
}

var contextSkipDirs = map[string]bool{
	".git": true, ".svn": true, ".hg": true,
	"node_modules": true, "vendor": true, "dist": true, "build": true,
	".next": true, ".turbo": true, ".idea": true, ".vscode": true,
	".apollo_contextplus": true, ".mcp_data": true,
}

type contextSymbol struct {
	Name      string
	Kind      string
	Line      int
	Signature string
}

type contextFileEntry struct {
	RelPath string
	AbsPath string
	Hash    string
	Header  string
	Symbols []contextSymbol
	Content string
}

type contextSearchCandidate struct {
	Entry        contextFileEntry
	KeywordScore float64
	Semantic     float64
	Combined     float64
}

type contextIdentifierEntry struct {
	File      contextFileEntry
	Symbol    contextSymbol
	Doc       string
	Keyword   float64
	Semantic  float64
	Combined  float64
	CallSites []string
}

type contextRestorePointFile struct {
	Path    string `json:"path"`
	Existed bool   `json:"existed"`
}

type contextRestorePoint struct {
	ID        string                    `json:"id"`
	Timestamp int64                     `json:"timestamp"`
	Message   string                    `json:"message"`
	Files     []contextRestorePointFile `json:"files"`
}

type contextHub struct {
	RelPath string
	AbsPath string
	Links   []string
}

type contextIndexedFile struct {
	RelPath string          `json:"rel_path"`
	Ext     string          `json:"ext"`
	Lang    string          `json:"lang"`
	Header  string          `json:"header"`
	Snippet string          `json:"snippet"`
	Symbols []contextSymbol `json:"symbols"`
	MTime   int64           `json:"mtime"`
	Size    int64           `json:"size"`
	Hash    string          `json:"hash"`
}

type contextProjectIndex struct {
	Version   int                           `json:"version"`
	Root      string                        `json:"root"`
	UpdatedAt int64                         `json:"updated_at"`
	Files     map[string]contextIndexedFile `json:"files"`
}

type contextSemanticNode struct {
	ID          string
	Depth       int
	FileIndices []int
	Children    []*contextSemanticNode
	Locked      bool
}

var contextLanguageByExt = map[string]string{
	".go": "go", ".ts": "typescript", ".tsx": "tsx", ".js": "javascript", ".jsx": "javascript",
	".py": "python", ".rs": "rust", ".java": "java", ".kt": "kotlin", ".swift": "swift",
	".c": "c", ".h": "c", ".cpp": "cpp", ".hpp": "cpp", ".cs": "csharp",
	".php": "php", ".rb": "ruby", ".scala": "scala", ".sql": "sql", ".sh": "bash",
	".yaml": "yaml", ".yml": "yaml",
}

var (
	contextEmbedMu      sync.Mutex
	contextEmbedCache   = map[string]map[string][]float64{}
	contextEmbedLoaded  = map[string]bool{}
	contextEmbedDirty   = map[string]bool{}
	contextEmbedSaving  = map[string]bool{}
	contextEmbedModel   = map[string]string{}
	contextEmbedBaseURL = map[string]string{}

	contextIndexMu       sync.Mutex
	contextIndexes       = map[string]*contextProjectIndex{}
	contextTrackerActive = map[string]bool{}
	contextPendingWarm   = map[string]map[string]bool{}
)

func executeGetContextTree(rawArgs string, projectRoot string) string {
	var args struct {
		TargetPath     string `json:"target_path"`
		DepthLimit     int    `json:"depth_limit"`
		IncludeSymbols *bool  `json:"include_symbols"`
		MaxTokens      int    `json:"max_tokens"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "Error: invalid arguments."
	}

	root := contextRoot(projectRoot)
	target := root
	if strings.TrimSpace(args.TargetPath) != "" {
		safePath, err := securePath(args.TargetPath, projectRoot)
		if err != nil {
			return err.Error()
		}
		target = safePath
	}
	if args.MaxTokens <= 0 {
		args.MaxTokens = 20000
	}
	_ = contextEnsureIndex(root)

	includeSymbols := true
	if args.IncludeSymbols != nil {
		includeSymbols = *args.IncludeSymbols
	}

	levels := []int{2, 1, 0}
	if !includeSymbols {
		levels = []int{1, 0}
	}

	var rendered string
	for _, level := range levels {
		rendered = contextRenderTree(target, root, args.DepthLimit, level)
		if contextEstimateTokens(rendered) <= args.MaxTokens {
			break
		}
	}

	if strings.TrimSpace(rendered) == "" {
		return "No files found for context tree."
	}

	if len(rendered) > 18000 {
		rendered = rendered[:18000] + "\n\n... [CONTEXT TREE TRUNCATED]"
	}
	return rendered
}

func executeGetFileSkeleton(rawArgs string, projectRoot string) string {
	var args struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil || strings.TrimSpace(args.FilePath) == "" {
		return "Error: 'file_path' is required."
	}

	safePath, err := securePath(args.FilePath, projectRoot)
	if err != nil {
		return err.Error()
	}

	data, err := os.ReadFile(safePath)
	if err != nil {
		return fmt.Sprintf("Error reading file: %v", err)
	}

	symbols := contextParseSymbolsByExt(strings.ToLower(filepath.Ext(safePath)), string(data))
	if len(symbols) == 0 {
		return fmt.Sprintf("No symbols found in %s", args.FilePath)
	}

	rel := contextRelativePath(contextRoot(projectRoot), safePath)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("File Skeleton: %s\n\n", rel))
	for _, s := range symbols {
		b.WriteString(fmt.Sprintf("- [%s] %s (line %d)\n  %s\n", s.Kind, s.Name, s.Line, s.Signature))
	}

	out := b.String()
	if len(out) > 18000 {
		out = out[:18000] + "\n\n... [SKELETON TRUNCATED]"
	}
	return out
}

func executeGetBlastRadius(rawArgs string, projectRoot string) string {
	var args struct {
		SymbolName  string `json:"symbol_name"`
		FileContext string `json:"file_context"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil || strings.TrimSpace(args.SymbolName) == "" {
		return "Error: 'symbol_name' is required."
	}

	entries, err := contextCollectFiles(contextRoot(projectRoot), contextRoot(projectRoot))
	if err != nil {
		return fmt.Sprintf("Error indexing files: %v", err)
	}

	wordRe := regexp.MustCompile(`\\b` + regexp.QuoteMeta(args.SymbolName) + `\\b`)
	exclude := strings.TrimSpace(args.FileContext)
	if exclude != "" {
		exclude = filepath.Clean(strings.TrimPrefix(exclude, "/"))
	}

	var hits []string
	for _, e := range entries {
		content, err := os.ReadFile(e.AbsPath)
		if err != nil {
			continue
		}
		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			if !wordRe.MatchString(line) {
				continue
			}
			if exclude != "" && filepath.Clean(e.RelPath) == exclude {
				symbols := e.Symbols
				isDefinition := false
				for _, s := range symbols {
					if s.Name == args.SymbolName && s.Line == i+1 {
						isDefinition = true
						break
					}
				}
				if isDefinition {
					continue
				}
			}
			hits = append(hits, fmt.Sprintf("%s:%d: %s", e.RelPath, i+1, strings.TrimSpace(line)))
			if len(hits) >= 400 {
				hits = append(hits, "... [ADDITIONAL REFERENCES TRUNCATED]")
				break
			}
		}
		if len(hits) >= 401 {
			break
		}
	}

	if len(hits) == 0 {
		return fmt.Sprintf("No references found for symbol '%s'.", args.SymbolName)
	}

	return fmt.Sprintf("Blast Radius for '%s' (%d hits):\n\n%s", args.SymbolName, len(hits), strings.Join(hits, "\n"))
}

func executeRunStaticAnalysisNative(rawArgs string, projectRoot string) string {
	var args struct {
		TargetPath string `json:"target_path"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "Error: invalid arguments."
	}

	root := contextRoot(projectRoot)
	target := root
	if strings.TrimSpace(args.TargetPath) != "" {
		safePath, err := securePath(args.TargetPath, projectRoot)
		if err != nil {
			return err.Error()
		}
		target = safePath
	}

	langs := contextDetectLanguages(target)
	if len(langs) == 0 {
		return "No supported source files found for static analysis."
	}

	var reports []string
	if langs["go"] && contextPathExists(filepath.Join(root, "go.mod")) {
		reports = append(reports, contextRunCheckCommand(root, "go vet", "go", "vet", "./..."))
	}
	if (langs["typescript"] || langs["javascript"]) && contextPathExists(filepath.Join(root, "package.json")) {
		if langs["typescript"] && contextPathExists(filepath.Join(root, "tsconfig.json")) {
			reports = append(reports, contextRunCheckCommand(root, "tsc --noEmit", "npx", "tsc", "--noEmit", "--pretty", "false"))
		}
		if contextPathExists(filepath.Join(root, "eslint.config.js")) || contextPathExists(filepath.Join(root, ".eslintrc")) || contextPathExists(filepath.Join(root, ".eslintrc.js")) {
			reports = append(reports, contextRunCheckCommand(root, "eslint", "npx", "eslint", ".", "--format", "compact"))
		}
	}
	if langs["python"] {
		pyFiles := contextFindFilesByExt(target, ".py", 80)
		if len(pyFiles) > 0 {
			args := []string{"-m", "py_compile"}
			args = append(args, pyFiles...)
			reports = append(reports, contextRunCheckCommand(root, "python -m py_compile", "python3", args...))
		}
	}
	if langs["rust"] && contextPathExists(filepath.Join(root, "Cargo.toml")) {
		reports = append(reports, contextRunCheckCommand(root, "cargo check", "cargo", "check", "--message-format=short"))
	}

	if len(reports) == 0 {
		return "No runnable static analyzers detected for this project path."
	}

	out := strings.Join(reports, "\n\n")
	if len(out) > 18000 {
		out = out[:18000] + "\n\n... [STATIC ANALYSIS OUTPUT TRUNCATED]"
	}
	return out
}

func executeSemanticCodeSearch(rawArgs string, projectRoot string) string {
	var args struct {
		Query                string   `json:"query"`
		TopK                 int      `json:"top_k"`
		SemanticWeight       *float64 `json:"semantic_weight"`
		KeywordWeight        *float64 `json:"keyword_weight"`
		MinSemanticScore     *float64 `json:"min_semantic_score"`
		MinKeywordScore      *float64 `json:"min_keyword_score"`
		MinCombinedScore     *float64 `json:"min_combined_score"`
		RequireKeywordMatch  *bool    `json:"require_keyword_match"`
		RequireSemanticMatch *bool    `json:"require_semantic_match"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil || strings.TrimSpace(args.Query) == "" {
		return "Error: 'query' is required."
	}

	if args.TopK <= 0 {
		args.TopK = 5
	}
	semW := 0.72
	kwW := 0.28
	if args.SemanticWeight != nil {
		semW = *args.SemanticWeight
	}
	if args.KeywordWeight != nil {
		kwW = *args.KeywordWeight
	}
	if semW < 0 {
		semW = 0
	}
	if kwW < 0 {
		kwW = 0
	}
	if semW == 0 && kwW == 0 {
		semW = 0.72
		kwW = 0.28
	}

	root := contextRoot(projectRoot)
	entries, err := contextCollectFiles(root, root)
	if err != nil {
		return fmt.Sprintf("Error indexing project: %v", err)
	}
	if len(entries) == 0 {
		return "No code files found for semantic search."
	}

	queryWords := contextSplitWords(args.Query)
	candidates := make([]contextSearchCandidate, 0, len(entries))
	for _, e := range entries {
		doc := contextFileDocForSearch(e)
		kwScore := contextKeywordScore(queryWords, doc)
		candidates = append(candidates, contextSearchCandidate{Entry: e, KeywordScore: kwScore})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].KeywordScore == candidates[j].KeywordScore {
			return candidates[i].Entry.RelPath < candidates[j].Entry.RelPath
		}
		return candidates[i].KeywordScore > candidates[j].KeywordScore
	})

	poolSize := 80
	if len(candidates) < poolSize {
		poolSize = len(candidates)
	}
	pool := candidates[:poolSize]

	queryVec, queryErr := contextEmbedding(root, "query:"+args.Query)
	for i := range pool {
		doc := contextFileDocForSearch(pool[i].Entry)
		if queryErr == nil && len(queryVec) > 0 {
			docVec, err := contextEmbedding(root, fmt.Sprintf("file:%s:%s:%s", pool[i].Entry.RelPath, pool[i].Entry.Hash, doc))
			if err == nil {
				pool[i].Semantic = contextCosine(queryVec, docVec)
			}
		}
		pool[i].Combined = semW*pool[i].Semantic + kwW*pool[i].KeywordScore
	}

	minSemantic := contextNormalizeThreshold(args.MinSemanticScore)
	minKeyword := contextNormalizeThreshold(args.MinKeywordScore)
	minCombined := contextNormalizeThreshold(args.MinCombinedScore)
	requireKeyword := args.RequireKeywordMatch != nil && *args.RequireKeywordMatch
	requireSemantic := args.RequireSemanticMatch != nil && *args.RequireSemanticMatch

	filtered := make([]contextSearchCandidate, 0, len(pool))
	for _, c := range pool {
		if c.Semantic < minSemantic {
			continue
		}
		if c.KeywordScore < minKeyword {
			continue
		}
		if c.Combined < minCombined {
			continue
		}
		if requireKeyword && c.KeywordScore <= 0 {
			continue
		}
		if requireSemantic && c.Semantic <= 0 {
			continue
		}
		filtered = append(filtered, c)
	}

	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Combined == filtered[j].Combined {
			return filtered[i].Entry.RelPath < filtered[j].Entry.RelPath
		}
		return filtered[i].Combined > filtered[j].Combined
	})

	if len(filtered) == 0 {
		return "No semantic matches found."
	}
	if len(filtered) > args.TopK {
		filtered = filtered[:args.TopK]
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Semantic Code Search: %s\n\n", args.Query))
	for i, c := range filtered {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, c.Entry.RelPath))
		b.WriteString(fmt.Sprintf("   combined=%.3f semantic=%.3f keyword=%.3f\n", c.Combined, c.Semantic, c.KeywordScore))
		if c.Entry.Header != "" {
			b.WriteString(fmt.Sprintf("   header: %s\n", c.Entry.Header))
		}
		if len(c.Entry.Symbols) > 0 {
			maxSymbols := 4
			if len(c.Entry.Symbols) < maxSymbols {
				maxSymbols = len(c.Entry.Symbols)
			}
			for si := 0; si < maxSymbols; si++ {
				s := c.Entry.Symbols[si]
				b.WriteString(fmt.Sprintf("   symbol: %s (%s) line %d\n", s.Name, s.Kind, s.Line))
			}
		}
		b.WriteString("\n")
	}

	out := b.String()
	if len(out) > 18000 {
		out = out[:18000] + "\n\n... [SEARCH OUTPUT TRUNCATED]"
	}
	return out
}

func executeSemanticIdentifierSearch(rawArgs string, projectRoot string) string {
	var args struct {
		Query                 string   `json:"query"`
		TopK                  int      `json:"top_k"`
		TopCallsPerIdentifier int      `json:"top_calls_per_identifier"`
		IncludeKinds          []string `json:"include_kinds"`
		SemanticWeight        *float64 `json:"semantic_weight"`
		KeywordWeight         *float64 `json:"keyword_weight"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil || strings.TrimSpace(args.Query) == "" {
		return "Error: 'query' is required."
	}
	if args.TopK <= 0 {
		args.TopK = 5
	}
	if args.TopCallsPerIdentifier <= 0 {
		args.TopCallsPerIdentifier = 10
	}

	semW := 0.78
	kwW := 0.22
	if args.SemanticWeight != nil {
		semW = *args.SemanticWeight
	}
	if args.KeywordWeight != nil {
		kwW = *args.KeywordWeight
	}

	allowedKinds := map[string]bool{}
	for _, k := range args.IncludeKinds {
		allowedKinds[strings.ToLower(strings.TrimSpace(k))] = true
	}

	root := contextRoot(projectRoot)
	entries, err := contextCollectFiles(root, root)
	if err != nil {
		return fmt.Sprintf("Error indexing project: %v", err)
	}

	var identifiers []contextIdentifierEntry
	queryWords := contextSplitWords(args.Query)
	for _, file := range entries {
		for _, sym := range file.Symbols {
			if len(allowedKinds) > 0 && !allowedKinds[strings.ToLower(sym.Kind)] {
				continue
			}
			doc := fmt.Sprintf("%s %s %s %s %s", sym.Name, sym.Kind, sym.Signature, file.Header, file.RelPath)
			kw := contextKeywordScore(queryWords, doc)
			identifiers = append(identifiers, contextIdentifierEntry{
				File:     file,
				Symbol:   sym,
				Doc:      doc,
				Keyword:  kw,
				Combined: kwW * kw,
			})
		}
	}

	if len(identifiers) == 0 {
		return "No identifiers found for semantic search."
	}

	sort.Slice(identifiers, func(i, j int) bool {
		if identifiers[i].Keyword == identifiers[j].Keyword {
			return identifiers[i].Symbol.Name < identifiers[j].Symbol.Name
		}
		return identifiers[i].Keyword > identifiers[j].Keyword
	})

	poolSize := 120
	if len(identifiers) < poolSize {
		poolSize = len(identifiers)
	}
	pool := identifiers[:poolSize]

	queryVec, queryErr := contextEmbedding(root, "identifier-query:"+args.Query)
	for i := range pool {
		if queryErr == nil && len(queryVec) > 0 {
			v, err := contextEmbedding(
				root,
				fmt.Sprintf("identifier:%s:%d:%s:%s", pool[i].File.RelPath, pool[i].Symbol.Line, pool[i].File.Hash, pool[i].Doc),
			)
			if err == nil {
				pool[i].Semantic = contextCosine(queryVec, v)
			}
		}
		pool[i].Combined = semW*pool[i].Semantic + kwW*pool[i].Keyword
	}

	sort.Slice(pool, func(i, j int) bool {
		if pool[i].Combined == pool[j].Combined {
			return pool[i].File.RelPath < pool[j].File.RelPath
		}
		return pool[i].Combined > pool[j].Combined
	})

	if len(pool) > args.TopK {
		pool = pool[:args.TopK]
	}

	allFiles := entries
	for i := range pool {
		pool[i].CallSites = contextFindCallSites(allFiles, pool[i].Symbol.Name, pool[i].File.RelPath, pool[i].Symbol.Line, args.TopCallsPerIdentifier)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Semantic Identifier Search: %s\n\n", args.Query))
	for i, r := range pool {
		b.WriteString(fmt.Sprintf("%d. %s (%s)\n", i+1, r.Symbol.Name, r.Symbol.Kind))
		b.WriteString(fmt.Sprintf("   file: %s:%d\n", r.File.RelPath, r.Symbol.Line))
		b.WriteString(fmt.Sprintf("   score: combined=%.3f semantic=%.3f keyword=%.3f\n", r.Combined, r.Semantic, r.Keyword))
		b.WriteString(fmt.Sprintf("   signature: %s\n", r.Symbol.Signature))
		if len(r.CallSites) > 0 {
			b.WriteString("   call sites:\n")
			for _, cs := range r.CallSites {
				b.WriteString("   - " + cs + "\n")
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func executeSemanticNavigate(rawArgs string, projectRoot string) string {
	var args struct {
		MaxDepth    int `json:"max_depth"`
		MaxClusters int `json:"max_clusters"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "Error: invalid arguments."
	}
	if args.MaxDepth <= 0 {
		args.MaxDepth = 3
	}
	if args.MaxClusters <= 0 {
		args.MaxClusters = 20
	}

	root := contextRoot(projectRoot)
	entries, err := contextCollectFiles(root, root)
	if err != nil {
		return fmt.Sprintf("Error indexing project: %v", err)
	}
	if len(entries) == 0 {
		return "No files found for semantic navigation."
	}

	vectors := contextBuildSemanticVectors(root, entries)
	if len(vectors) != len(entries) || len(vectors) == 0 {
		return "Unable to build semantic vectors for navigation."
	}

	const maxSpectralFiles = 220
	coreIndices := make([]int, 0, len(entries))
	for i := range entries {
		coreIndices = append(coreIndices, i)
	}
	sampled := false
	if len(coreIndices) > maxSpectralFiles {
		coreIndices = contextSelectDiverseIndices(vectors, maxSpectralFiles)
		sampled = true
	}

	tree := &contextSemanticNode{
		ID:          "1",
		Depth:       1,
		FileIndices: append([]int(nil), coreIndices...),
	}
	leafCount := 1
	for leafCount < args.MaxClusters {
		candidate := contextSelectSplitCandidate(tree, args.MaxDepth)
		if candidate == nil {
			break
		}
		left, right, ok := contextSpectralBisect(vectors, candidate.FileIndices)
		if !ok {
			candidate.Locked = true
			continue
		}
		candidate.Children = []*contextSemanticNode{
			{ID: candidate.ID + ".1", Depth: candidate.Depth + 1, FileIndices: left},
			{ID: candidate.ID + ".2", Depth: candidate.Depth + 1, FileIndices: right},
		}
		leafCount++
	}

	if sampled {
		coreSet := map[int]bool{}
		for _, idx := range coreIndices {
			coreSet[idx] = true
		}
		leaves := contextCollectLeafNodes(tree)
		centroids := make([][]float64, len(leaves))
		for i, leaf := range leaves {
			centroids[i] = contextCentroidForIndices(vectors, leaf.FileIndices)
		}
		for idx := range entries {
			if coreSet[idx] {
				continue
			}
			bestLeaf := 0
			bestScore := -2.0
			for li := range leaves {
				score := contextCosine(vectors[idx], centroids[li])
				if score > bestScore {
					bestScore = score
					bestLeaf = li
				}
			}
			leaves[bestLeaf].FileIndices = append(leaves[bestLeaf].FileIndices, idx)
		}
	}

	for _, leaf := range contextCollectLeafNodes(tree) {
		sort.Ints(leaf.FileIndices)
	}

	var b strings.Builder
	b.WriteString("Semantic Navigate (spectral clustering)\n\n")
	b.WriteString(fmt.Sprintf("files: %d", len(entries)))
	if sampled {
		b.WriteString(fmt.Sprintf(", spectral-core: %d", len(coreIndices)))
	}
	b.WriteString(fmt.Sprintf(", depth<=%d, max_clusters=%d\n\n", args.MaxDepth, args.MaxClusters))
	contextRenderSemanticNode(&b, tree, entries, 0)

	out := b.String()
	if len(out) > 18000 {
		out = out[:18000] + "\n\n... [SEMANTIC NAVIGATION TRUNCATED]"
	}
	return out
}

func executeGetFeatureHub(rawArgs string, projectRoot string) string {
	var args struct {
		HubPath    string `json:"hub_path"`
		Feature    string `json:"feature_name"`
		ShowOrphan *bool  `json:"show_orphans"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "Error: invalid arguments."
	}

	root := contextRoot(projectRoot)
	hubs, err := contextCollectHubs(root)
	if err != nil {
		return fmt.Sprintf("Error loading hubs: %v", err)
	}

	if args.ShowOrphan != nil && *args.ShowOrphan {
		orphans := contextFindHubOrphans(root, hubs)
		if len(orphans) == 0 {
			return "No orphaned source files found."
		}
		if len(orphans) > 300 {
			orphans = append(orphans[:300], "... [ADDITIONAL ORPHANS TRUNCATED]")
		}
		return "Orphan Source Files:\n\n" + strings.Join(orphans, "\n")
	}

	if strings.TrimSpace(args.HubPath) == "" && strings.TrimSpace(args.Feature) == "" {
		if len(hubs) == 0 {
			return "No feature hubs found (.md files with [[wikilinks]])."
		}
		var lines []string
		for _, h := range hubs {
			lines = append(lines, fmt.Sprintf("- %s (%d links)", h.RelPath, len(h.Links)))
		}
		sort.Strings(lines)
		return "Feature Hubs:\n\n" + strings.Join(lines, "\n")
	}

	selected := contextSelectHub(root, hubs, strings.TrimSpace(args.HubPath), strings.TrimSpace(args.Feature))
	if selected == nil {
		return "No matching feature hub found."
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Feature Hub: %s\n\n", selected.RelPath))
	if len(selected.Links) == 0 {
		b.WriteString("No wikilinks found in this hub.")
		return b.String()
	}
	for _, l := range selected.Links {
		resolved := contextResolveHubLink(root, selected.AbsPath, l)
		if resolved == "" {
			b.WriteString(fmt.Sprintf("- %s (unresolved)\n", l))
			continue
		}
		rel := contextRelativePath(root, resolved)
		b.WriteString(fmt.Sprintf("- %s\n", rel))
		if data, err := os.ReadFile(resolved); err == nil {
			syms := contextParseSymbolsByExt(strings.ToLower(filepath.Ext(resolved)), string(data))
			if len(syms) > 0 {
				maxSymbols := 4
				if len(syms) < maxSymbols {
					maxSymbols = len(syms)
				}
				for i := 0; i < maxSymbols; i++ {
					s := syms[i]
					b.WriteString(fmt.Sprintf("  - %s (%s) line %d\n", s.Name, s.Kind, s.Line))
				}
			}
		}
	}

	out := b.String()
	if len(out) > 18000 {
		out = out[:18000] + "\n\n... [FEATURE HUB OUTPUT TRUNCATED]"
	}
	return out
}

func executeProposeCommitNative(rawArgs string, projectRoot string) string {
	var args struct {
		FilePath   string `json:"file_path"`
		NewContent string `json:"new_content"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil || strings.TrimSpace(args.FilePath) == "" {
		return "Error: 'file_path' and 'new_content' are required."
	}
	if strings.TrimSpace(args.NewContent) == "" {
		return "Error: new content is empty."
	}

	lineCount := strings.Count(args.NewContent, "\n") + 1
	if lineCount > 5000 {
		return fmt.Sprintf("Error: file has %d lines; exceeds safety limit of 5000.", lineCount)
	}

	root := contextRoot(projectRoot)
	safePath, err := securePath(args.FilePath, projectRoot)
	if err != nil {
		return err.Error()
	}
	rel := contextRelativePath(root, safePath)

	rpID, err := contextCreateRestorePoint(root, []string{rel}, "propose_commit before write")
	if err != nil {
		return fmt.Sprintf("Error creating restore point: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(safePath), 0o755); err != nil {
		return fmt.Sprintf("Error preparing directory: %v", err)
	}
	if err := os.WriteFile(safePath, []byte(args.NewContent), 0o644); err != nil {
		return fmt.Sprintf("Error writing file: %v", err)
	}
	_ = contextEnsureIndex(root)

	warnings := contextValidateCommitContent(args.NewContent)
	if warnings != "" {
		return fmt.Sprintf("Saved %s\nRestore point: %s\n\nValidation warnings:\n%s", rel, rpID, warnings)
	}
	return fmt.Sprintf("Saved %s\nRestore point: %s", rel, rpID)
}

func executeListRestorePointsNative(rawArgs string, projectRoot string) string {
	root := contextRoot(projectRoot)
	points, err := contextListRestorePoints(root)
	if err != nil {
		return fmt.Sprintf("Error listing restore points: %v", err)
	}
	if len(points) == 0 {
		return "No restore points found."
	}

	var lines []string
	for _, p := range points {
		var fileNames []string
		for _, f := range p.Files {
			fileNames = append(fileNames, f.Path)
		}
		lines = append(lines, fmt.Sprintf("%s | %s | %s | %s", p.ID, time.UnixMilli(p.Timestamp).Format(time.RFC3339), strings.Join(fileNames, ", "), p.Message))
	}
	return fmt.Sprintf("Restore Points (%d):\n\n%s", len(lines), strings.Join(lines, "\n"))
}

func executeUndoChangeNative(rawArgs string, projectRoot string) string {
	var args struct {
		PointID string `json:"point_id"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil || strings.TrimSpace(args.PointID) == "" {
		return "Error: 'point_id' is required."
	}

	root := contextRoot(projectRoot)
	restored, err := contextRestorePointByID(root, args.PointID)
	if err != nil {
		return fmt.Sprintf("Error restoring point: %v", err)
	}
	_ = contextEnsureIndex(root)
	if len(restored) == 0 {
		return "No files restored."
	}
	return fmt.Sprintf("Restored %d file(s):\n%s", len(restored), strings.Join(restored, "\n"))
}

func contextRoot(projectRoot string) string {
	if strings.TrimSpace(projectRoot) != "" {
		return projectRoot
	}
	return WorkspaceDir
}

func contextEstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	return (len(s) / 4) + 1
}

func contextRenderTree(target string, root string, depthLimit int, level int) string {
	info, err := os.Stat(target)
	if err != nil {
		return ""
	}

	var b strings.Builder
	if info.IsDir() {
		rel := contextRelativePath(root, target)
		if rel == "." || rel == "" {
			rel = "/"
		}
		b.WriteString(fmt.Sprintf("Context Tree (%s)\n\n", rel))
		contextRenderTreeDir(&b, target, root, 0, depthLimit, level)
	} else {
		rel := contextRelativePath(root, target)
		b.WriteString(fmt.Sprintf("Context Tree (%s)\n\n", rel))
		contextRenderTreeFile(&b, target, rel, root, level, "")
	}
	return b.String()
}

func contextRenderTreeDir(b *strings.Builder, dir string, root string, depth int, depthLimit int, level int) {
	if depthLimit > 0 && depth > depthLimit {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && name != ".env" && name != ".github" {
			if contextSkipDirs[name] {
				continue
			}
		}
		if e.IsDir() {
			if contextSkipDirs[name] {
				continue
			}
			indent := strings.Repeat("  ", depth)
			b.WriteString(fmt.Sprintf("%s- %s/\n", indent, name))
			contextRenderTreeDir(b, filepath.Join(dir, name), root, depth+1, depthLimit, level)
			continue
		}

		abs := filepath.Join(dir, name)
		ext := strings.ToLower(filepath.Ext(abs))
		if !contextCodeExt[ext] && ext != ".md" {
			continue
		}
		rel := contextRelativePath(root, abs)
		indent := strings.Repeat("  ", depth)
		contextRenderTreeFile(b, abs, rel, root, level, indent)
	}
}

func contextRenderTreeFile(b *strings.Builder, abs string, rel string, root string, level int, indent string) {
	if level <= 0 {
		b.WriteString(fmt.Sprintf("%s- %s\n", indent, rel))
		return
	}

	header := ""
	var symbols []contextSymbol
	if idxFile, ok := contextGetIndexedFile(root, rel); ok {
		header = idxFile.Header
		symbols = idxFile.Symbols
	}
	if header == "" || (level > 1 && len(symbols) == 0) {
		data, err := os.ReadFile(abs)
		if err == nil {
			content := string(data)
			header = contextFileHeader(content)
			if level > 1 {
				symbols = contextParseSymbolsByExt(strings.ToLower(filepath.Ext(abs)), content)
			}
		}
	}
	if header != "" {
		b.WriteString(fmt.Sprintf("%s- %s :: %s\n", indent, rel, header))
	} else {
		b.WriteString(fmt.Sprintf("%s- %s\n", indent, rel))
	}
	if level <= 1 {
		return
	}

	maxSymbols := 8
	if len(symbols) < maxSymbols {
		maxSymbols = len(symbols)
	}
	for i := 0; i < maxSymbols; i++ {
		s := symbols[i]
		b.WriteString(fmt.Sprintf("%s  - %s %s:%d\n", indent, s.Kind, s.Name, s.Line))
	}
}

func contextCollectFiles(root string, target string) ([]contextFileEntry, error) {
	if err := contextEnsureIndex(root); err != nil {
		return nil, err
	}

	targetAbs := filepath.Clean(target)
	if targetAbs == "" {
		targetAbs = root
	}

	contextIndexMu.Lock()
	idx := contextIndexes[root]
	if idx == nil {
		contextIndexMu.Unlock()
		return []contextFileEntry{}, nil
	}
	entries := make([]contextFileEntry, 0, len(idx.Files))
	for rel, f := range idx.Files {
		abs := filepath.Join(root, rel)
		if !contextPathWithinTarget(abs, targetAbs) {
			continue
		}
		entries = append(entries, contextFileEntry{
			RelPath: rel,
			AbsPath: abs,
			Hash:    f.Hash,
			Header:  f.Header,
			Symbols: f.Symbols,
			Content: f.Snippet,
		})
	}
	contextIndexMu.Unlock()

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].RelPath < entries[j].RelPath
	})
	return entries, nil
}

func contextLooksText(content string) bool {
	if content == "" {
		return true
	}
	if strings.IndexByte(content, 0) >= 0 {
		return false
	}
	return true
}

func contextFileHeader(content string) string {
	lines := strings.Split(content, "\n")
	parts := make([]string, 0, 2)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts = append(parts, line)
		if len(parts) == 2 {
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	header := strings.Join(parts, " | ")
	if len(header) > 160 {
		header = header[:160]
	}
	return header
}

func contextParseSymbolsByExt(ext string, content string) []contextSymbol {
	tsSymbols, ok := contextParseSymbolsByTreeSitter(ext, content)
	regexSymbols := contextParseSymbolsRegexByExt(ext, content)
	if ok && len(tsSymbols) > 0 {
		return contextMergeSymbols(tsSymbols, regexSymbols)
	}
	return regexSymbols
}

func contextParseSymbolsRegexByExt(ext string, content string) []contextSymbol {
	lines := strings.Split(content, "\n")
	var out []contextSymbol

	appendMatch := func(line string, idx int, kind string, re *regexp.Regexp) {
		m := re.FindStringSubmatch(line)
		if len(m) < 2 {
			return
		}
		sig := strings.TrimSpace(line)
		if len(sig) > 200 {
			sig = sig[:200]
		}
		out = append(out, contextSymbol{Name: m[1], Kind: kind, Line: idx + 1, Signature: sig})
	}

	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		switch ext {
		case ".go":
			appendMatch(line, i, "function", regexp.MustCompile(`^func\\s+(?:\\([^)]*\\)\\s*)?([A-Za-z_][A-Za-z0-9_]*)\\s*\\(`))
			appendMatch(line, i, "type", regexp.MustCompile(`^type\\s+([A-Za-z_][A-Za-z0-9_]*)\\s+(?:struct|interface)\\b`))
			appendMatch(line, i, "const", regexp.MustCompile(`^const\\s+([A-Za-z_][A-Za-z0-9_]*)\\b`))
			appendMatch(line, i, "variable", regexp.MustCompile(`^var\\s+([A-Za-z_][A-Za-z0-9_]*)\\b`))
		case ".ts", ".tsx", ".js", ".jsx":
			appendMatch(line, i, "function", regexp.MustCompile(`^function\\s+([A-Za-z_][A-Za-z0-9_]*)\\s*\\(`))
			appendMatch(line, i, "function", regexp.MustCompile(`^(?:export\\s+)?const\\s+([A-Za-z_][A-Za-z0-9_]*)\\s*=\\s*(?:async\\s*)?\\(`))
			appendMatch(line, i, "class", regexp.MustCompile(`^class\\s+([A-Za-z_][A-Za-z0-9_]*)\\b`))
			appendMatch(line, i, "class", regexp.MustCompile(`^export\\s+class\\s+([A-Za-z_][A-Za-z0-9_]*)\\b`))
			appendMatch(line, i, "interface", regexp.MustCompile(`^interface\\s+([A-Za-z_][A-Za-z0-9_]*)\\b`))
			appendMatch(line, i, "type", regexp.MustCompile(`^type\\s+([A-Za-z_][A-Za-z0-9_]*)\\s*=`))
		case ".py":
			appendMatch(line, i, "function", regexp.MustCompile(`^def\\s+([A-Za-z_][A-Za-z0-9_]*)\\s*\\(`))
			appendMatch(line, i, "class", regexp.MustCompile(`^class\\s+([A-Za-z_][A-Za-z0-9_]*)\\b`))
		case ".rs":
			appendMatch(line, i, "function", regexp.MustCompile(`^fn\\s+([A-Za-z_][A-Za-z0-9_]*)\\s*\\(`))
			appendMatch(line, i, "struct", regexp.MustCompile(`^struct\\s+([A-Za-z_][A-Za-z0-9_]*)\\b`))
			appendMatch(line, i, "enum", regexp.MustCompile(`^enum\\s+([A-Za-z_][A-Za-z0-9_]*)\\b`))
			appendMatch(line, i, "impl", regexp.MustCompile(`^impl\\s+([A-Za-z_][A-Za-z0-9_]*)\\b`))
		default:
			appendMatch(line, i, "function", regexp.MustCompile(`^(?:public|private|protected|static|async|final|export)?\\s*([A-Za-z_][A-Za-z0-9_]*)\\s*\\(`))
			appendMatch(line, i, "class", regexp.MustCompile(`^class\\s+([A-Za-z_][A-Za-z0-9_]*)\\b`))
		}
	}

	if len(out) > 400 {
		out = out[:400]
	}
	return out
}

func contextParseSymbolsByTreeSitter(ext string, content string) ([]contextSymbol, bool) {
	lang := contextTreeSitterLanguage(ext)
	if lang == nil {
		return nil, false
	}
	source := []byte(content)
	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(lang)

	tree, err := parser.ParseCtx(context.Background(), nil, source)
	if err != nil || tree == nil {
		return nil, false
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		return nil, true
	}

	var symbols []contextSymbol
	contextCollectTreeSitterSymbols(root, source, ext, &symbols)
	if len(symbols) > 400 {
		symbols = symbols[:400]
	}
	return symbols, true
}

func contextCollectTreeSitterSymbols(node *sitter.Node, source []byte, ext string, out *[]contextSymbol) {
	if node == nil {
		return
	}
	kind, name := contextExtractTreeSitterSymbol(node, source, ext)
	if name != "" {
		lineText := contextLineByNumber(string(source), int(node.StartPoint().Row)+1)
		if len(lineText) > 220 {
			lineText = lineText[:220]
		}
		*out = append(*out, contextSymbol{
			Name:      name,
			Kind:      kind,
			Line:      int(node.StartPoint().Row) + 1,
			Signature: strings.TrimSpace(lineText),
		})
	}
	for i := uint32(0); i < node.NamedChildCount(); i++ {
		contextCollectTreeSitterSymbols(node.NamedChild(int(i)), source, ext, out)
	}
}

func contextExtractTreeSitterSymbol(node *sitter.Node, source []byte, ext string) (string, string) {
	nodeType := node.Type()
	switch ext {
	case ".go":
		switch nodeType {
		case "function_declaration", "method_declaration":
			if n := node.ChildByFieldName("name"); n != nil {
				return "function", strings.TrimSpace(n.Content(source))
			}
		case "type_spec":
			if n := node.ChildByFieldName("name"); n != nil {
				return "type", strings.TrimSpace(n.Content(source))
			}
		case "const_spec":
			if n := node.ChildByFieldName("name"); n != nil {
				return "const", strings.TrimSpace(n.Content(source))
			}
		case "var_spec":
			if n := node.ChildByFieldName("name"); n != nil {
				return "variable", strings.TrimSpace(n.Content(source))
			}
		}
	case ".ts", ".tsx", ".js", ".jsx":
		switch nodeType {
		case "function_declaration", "generator_function_declaration":
			if n := node.ChildByFieldName("name"); n != nil {
				return "function", strings.TrimSpace(n.Content(source))
			}
		case "method_definition":
			if n := node.ChildByFieldName("name"); n != nil {
				return "method", strings.TrimSpace(n.Content(source))
			}
		case "class_declaration":
			if n := node.ChildByFieldName("name"); n != nil {
				return "class", strings.TrimSpace(n.Content(source))
			}
		case "interface_declaration":
			if n := node.ChildByFieldName("name"); n != nil {
				return "interface", strings.TrimSpace(n.Content(source))
			}
		case "type_alias_declaration":
			if n := node.ChildByFieldName("name"); n != nil {
				return "type", strings.TrimSpace(n.Content(source))
			}
		case "variable_declarator":
			n := node.ChildByFieldName("name")
			v := node.ChildByFieldName("value")
			if n != nil && v != nil {
				vt := v.Type()
				if vt == "arrow_function" || vt == "function_expression" || vt == "generator_function" || vt == "method_definition" {
					return "function", strings.TrimSpace(n.Content(source))
				}
			}
		}
	case ".py":
		switch nodeType {
		case "function_definition":
			if n := node.ChildByFieldName("name"); n != nil {
				return "function", strings.TrimSpace(n.Content(source))
			}
		case "class_definition":
			if n := node.ChildByFieldName("name"); n != nil {
				return "class", strings.TrimSpace(n.Content(source))
			}
		}
	case ".rs":
		switch nodeType {
		case "function_item":
			if n := node.ChildByFieldName("name"); n != nil {
				return "function", strings.TrimSpace(n.Content(source))
			}
		case "struct_item":
			if n := node.ChildByFieldName("name"); n != nil {
				return "struct", strings.TrimSpace(n.Content(source))
			}
		case "enum_item":
			if n := node.ChildByFieldName("name"); n != nil {
				return "enum", strings.TrimSpace(n.Content(source))
			}
		case "trait_item":
			if n := node.ChildByFieldName("name"); n != nil {
				return "trait", strings.TrimSpace(n.Content(source))
			}
		}
	default:
		switch nodeType {
		case "function_definition", "function_declaration", "method_definition", "class_declaration":
			if n := node.ChildByFieldName("name"); n != nil {
				kind := "function"
				if nodeType == "class_declaration" {
					kind = "class"
				} else if nodeType == "method_definition" {
					kind = "method"
				}
				return kind, strings.TrimSpace(n.Content(source))
			}
		}
	}
	return "", ""
}

func contextTreeSitterLanguage(ext string) *sitter.Language {
	switch ext {
	case ".go":
		return sittergolang.GetLanguage()
	case ".ts":
		return sittertypescript.GetLanguage()
	case ".tsx":
		return sittertsx.GetLanguage()
	case ".js", ".jsx":
		return sitterjavascript.GetLanguage()
	case ".py":
		return sitterpython.GetLanguage()
	case ".rs":
		return sitterrust.GetLanguage()
	case ".java":
		return sitterjava.GetLanguage()
	case ".kt":
		return sitterkotlin.GetLanguage()
	case ".swift":
		return sitterswift.GetLanguage()
	case ".c", ".h":
		return sitterc.GetLanguage()
	case ".cpp", ".hpp":
		return sittercpp.GetLanguage()
	case ".cs":
		return sittercsharp.GetLanguage()
	case ".php":
		return sitterphp.GetLanguage()
	case ".rb":
		return sitterruby.GetLanguage()
	case ".scala":
		return sitterscala.GetLanguage()
	case ".sql":
		return sittersql.GetLanguage()
	case ".sh":
		return sitterbash.GetLanguage()
	case ".yaml", ".yml":
		return sitteryaml.GetLanguage()
	default:
		return nil
	}
}

func contextMergeSymbols(primary []contextSymbol, fallback []contextSymbol) []contextSymbol {
	seen := map[string]bool{}
	out := make([]contextSymbol, 0, len(primary)+len(fallback))
	for _, s := range primary {
		key := strings.ToLower(fmt.Sprintf("%s|%s|%d", s.Kind, s.Name, s.Line))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	for _, s := range fallback {
		key := strings.ToLower(fmt.Sprintf("%s|%s|%d", s.Kind, s.Name, s.Line))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Line == out[j].Line {
			return out[i].Name < out[j].Name
		}
		return out[i].Line < out[j].Line
	})
	if len(out) > 400 {
		out = out[:400]
	}
	return out
}

func contextFileDocForSearch(e contextFileEntry) string {
	var symbolNames []string
	for i, s := range e.Symbols {
		if i >= 16 {
			break
		}
		symbolNames = append(symbolNames, s.Name)
	}
	prefix := e.Content
	if len(prefix) > 1200 {
		prefix = prefix[:1200]
	}
	return strings.ToLower(strings.Join([]string{e.RelPath, e.Header, strings.Join(symbolNames, " "), prefix}, "\n"))
}

func contextSplitWords(s string) []string {
	s = strings.ToLower(s)
	re := regexp.MustCompile(`[a-z0-9_]+`)
	words := re.FindAllString(s, -1)
	seen := map[string]bool{}
	var out []string
	for _, w := range words {
		if len(w) < 2 || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}

func contextKeywordScore(queryWords []string, doc string) float64 {
	if len(queryWords) == 0 || doc == "" {
		return 0
	}
	doc = strings.ToLower(doc)
	hits := 0.0
	for _, w := range queryWords {
		if strings.Contains(doc, w) {
			hits += 1.0
		}
	}
	return hits / float64(len(queryWords))
}

func contextNormalizeThreshold(v *float64) float64 {
	if v == nil {
		return 0
	}
	value := *v
	if value > 1 {
		value = value / 100.0
	}
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func contextCosine(a []float64, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	dot := 0.0
	normA := 0.0
	normB := 0.0
	for i := 0; i < len(a); i++ {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func contextEmbedding(root string, text string) ([]float64, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("empty text")
	}

	baseURL := contextGetSetting("embedding_api_url", "http://localhost:11434")
	model := contextGetSetting("embedding_model", "nomic-embed-text")
	cacheKey := contextEmbeddingKey(baseURL, model, text)

	contextEmbedMu.Lock()
	contextEmbedModel[root] = model
	contextEmbedBaseURL[root] = baseURL
	if !contextEmbedLoaded[root] {
		contextEmbedCache[root] = contextLoadEmbeddingCache(root)
		contextEmbedLoaded[root] = true
	}
	rootCache := contextEmbedCache[root]
	if v, ok := rootCache[cacheKey]; ok {
		contextEmbedMu.Unlock()
		return v, nil
	}
	contextEmbedMu.Unlock()

	isOpenAICompatible := strings.Contains(baseURL, "/v1") || strings.Contains(baseURL, "openrouter.ai")
	endpoint := strings.TrimRight(baseURL, "/") + "/api/embeddings"
	payload := map[string]interface{}{"model": model, "prompt": text}
	if isOpenAICompatible {
		endpoint = strings.TrimRight(baseURL, "/") + "/embeddings"
		payload = map[string]interface{}{"model": model, "input": text}
	}
	b, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", endpoint, bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	if isOpenAICompatible {
		apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
		if apiKey == "" || apiKey == "your_openrouter_api_key_here" {
			apiKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
		}
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}
	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("embedding API status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var ollamaOut struct {
		Embedding []float64 `json:"embedding"`
	}
	var embedding []float64
	if err := json.Unmarshal(body, &ollamaOut); err == nil && len(ollamaOut.Embedding) > 0 {
		embedding = ollamaOut.Embedding
	} else {
		var openAIOut struct {
			Data []struct {
				Embedding []float64 `json:"embedding"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &openAIOut); err != nil {
			return nil, err
		}
		if len(openAIOut.Data) > 0 {
			embedding = openAIOut.Data[0].Embedding
		}
	}
	if len(embedding) == 0 {
		return nil, fmt.Errorf("empty embedding vector")
	}

	contextEmbedMu.Lock()
	if _, ok := contextEmbedCache[root]; !ok {
		contextEmbedCache[root] = map[string][]float64{}
	}
	contextEmbedCache[root][cacheKey] = embedding
	contextEmbedDirty[root] = true
	shouldSave := !contextEmbedSaving[root]
	if shouldSave {
		contextEmbedSaving[root] = true
	}
	contextEmbedMu.Unlock()

	if shouldSave {
		go contextPersistEmbeddingCache(root)
	}

	return embedding, nil
}

func contextEmbeddingKey(baseURL string, model string, text string) string {
	h := sha1.Sum([]byte(baseURL + "|" + model + "|" + text))
	return hex.EncodeToString(h[:])
}

func contextLoadEmbeddingCache(root string) map[string][]float64 {
	cachePath := filepath.Join(root, ".apollo_contextplus", "embeddings_cache.json")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return map[string][]float64{}
	}
	var m map[string][]float64
	if json.Unmarshal(data, &m) != nil {
		return map[string][]float64{}
	}
	if m == nil {
		return map[string][]float64{}
	}
	return m
}

func contextPersistEmbeddingCache(root string) {
	defer func() {
		contextEmbedMu.Lock()
		contextEmbedSaving[root] = false
		if contextEmbedDirty[root] {
			contextEmbedDirty[root] = false
			contextEmbedSaving[root] = true
			contextEmbedMu.Unlock()
			go contextPersistEmbeddingCache(root)
			return
		}
		contextEmbedMu.Unlock()
	}()

	time.Sleep(300 * time.Millisecond)

	contextEmbedMu.Lock()
	if !contextEmbedDirty[root] {
		contextEmbedMu.Unlock()
		return
	}
	cacheCopy := make(map[string][]float64, len(contextEmbedCache))
	rootCache, ok := contextEmbedCache[root]
	if !ok {
		contextEmbedMu.Unlock()
		return
	}
	cacheCopy = make(map[string][]float64, len(rootCache))
	for k, v := range rootCache {
		cacheCopy[k] = v
	}
	contextEmbedDirty[root] = false
	contextEmbedMu.Unlock()

	data, err := json.Marshal(cacheCopy)
	if err != nil {
		return
	}
	baseDir := filepath.Join(root, ".apollo_contextplus")
	_ = os.MkdirAll(baseDir, 0o755)
	_ = os.WriteFile(filepath.Join(baseDir, "embeddings_cache.json"), data, 0o644)
}

func contextGetSetting(key string, def string) string {
	var val string
	err := db.DB.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	if err != nil || strings.TrimSpace(val) == "" {
		return def
	}
	return strings.TrimSpace(val)
}

func contextFindCallSites(entries []contextFileEntry, symbol string, defFile string, defLine int, limit int) []string {
	if limit <= 0 {
		limit = 10
	}
	re := regexp.MustCompile(`\\b` + regexp.QuoteMeta(symbol) + `\\b`)
	var sites []string
	for _, e := range entries {
		content, err := os.ReadFile(e.AbsPath)
		if err != nil {
			continue
		}
		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			if !re.MatchString(line) {
				continue
			}
			if e.RelPath == defFile && i+1 == defLine {
				continue
			}
			sites = append(sites, fmt.Sprintf("%s:%d: %s", e.RelPath, i+1, strings.TrimSpace(line)))
			if len(sites) >= limit {
				return sites
			}
		}
	}
	return sites
}

func contextTopTerms(files []contextFileEntry, max int) []string {
	stop := map[string]bool{
		"the": true, "and": true, "for": true, "with": true, "from": true,
		"const": true, "func": true, "function": true, "class": true,
		"type": true, "import": true, "export": true, "return": true,
	}
	counts := map[string]int{}
	for _, f := range files {
		tokens := contextSplitWords(f.RelPath + " " + f.Header)
		for _, t := range tokens {
			if len(t) < 3 || stop[t] {
				continue
			}
			counts[t]++
		}
	}
	type kv struct {
		K string
		V int
	}
	var list []kv
	for k, v := range counts {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].V > list[j].V })
	if len(list) > max {
		list = list[:max]
	}
	var out []string
	for _, item := range list {
		out = append(out, item.K)
	}
	return out
}

func contextBuildSemanticVectors(root string, entries []contextFileEntry) [][]float64 {
	raw := make([][]float64, len(entries))
	docs := make([]string, len(entries))
	targetDim := 0

	for i, e := range entries {
		doc := contextFileDocForSearch(e)
		docs[i] = doc
		vec, err := contextEmbedding(root, fmt.Sprintf("file:%s:%s:%s", e.RelPath, e.Hash, doc))
		if err == nil && len(vec) > 0 {
			raw[i] = vec
			if targetDim == 0 {
				targetDim = len(vec)
			}
		}
	}

	if targetDim == 0 {
		targetDim = 96
	}

	out := make([][]float64, len(entries))
	for i := range entries {
		if len(raw[i]) == targetDim {
			out[i] = contextNormalizeVector(raw[i])
			continue
		}
		if len(raw[i]) > 0 {
			out[i] = contextNormalizeVector(contextResizeVector(raw[i], targetDim))
			continue
		}
		out[i] = contextLexicalVector(docs[i], targetDim)
	}
	return out
}

func contextResizeVector(vec []float64, size int) []float64 {
	if size <= 0 {
		return []float64{}
	}
	out := make([]float64, size)
	if len(vec) == 0 {
		return out
	}
	if len(vec) >= size {
		copy(out, vec[:size])
		return out
	}
	copy(out, vec)
	return out
}

func contextLexicalVector(doc string, dim int) []float64 {
	vec := make([]float64, dim)
	if dim <= 0 {
		return vec
	}
	words := contextSplitWords(doc)
	if len(words) == 0 {
		return vec
	}
	for _, w := range words {
		h := sha1.Sum([]byte(w))
		i1 := (int(h[0])<<8 | int(h[1])) % dim
		i2 := (int(h[2])<<8 | int(h[3])) % dim
		s1 := 1.0
		s2 := 1.0
		if h[4]%2 == 0 {
			s1 = -1
		}
		if h[5]%2 == 0 {
			s2 = -1
		}
		vec[i1] += s1
		vec[i2] += s2
	}
	return contextNormalizeVector(vec)
}

func contextNormalizeVector(vec []float64) []float64 {
	out := make([]float64, len(vec))
	copy(out, vec)
	norm := 0.0
	for _, v := range out {
		norm += v * v
	}
	if norm == 0 {
		return out
	}
	norm = math.Sqrt(norm)
	for i := range out {
		out[i] /= norm
	}
	return out
}

func contextSelectDiverseIndices(vectors [][]float64, limit int) []int {
	n := len(vectors)
	if n == 0 || limit <= 0 {
		return nil
	}
	if n <= limit {
		out := make([]int, n)
		for i := range vectors {
			out[i] = i
		}
		return out
	}

	start := 0
	bestNorm := -1.0
	for i, v := range vectors {
		norm := 0.0
		for _, x := range v {
			norm += x * x
		}
		if norm > bestNorm {
			bestNorm = norm
			start = i
		}
	}

	selected := []int{start}
	selectedSet := map[int]bool{start: true}
	minDist := make([]float64, n)
	for i := range minDist {
		minDist[i] = math.Inf(1)
	}

	for len(selected) < limit {
		last := selected[len(selected)-1]
		for i := 0; i < n; i++ {
			if selectedSet[i] {
				continue
			}
			dist := 1.0 - contextCosine(vectors[i], vectors[last])
			if dist < minDist[i] {
				minDist[i] = dist
			}
		}

		next := -1
		nextDist := -1.0
		for i := 0; i < n; i++ {
			if selectedSet[i] {
				continue
			}
			if minDist[i] > nextDist {
				nextDist = minDist[i]
				next = i
			}
		}
		if next < 0 {
			break
		}
		selected = append(selected, next)
		selectedSet[next] = true
	}

	sort.Ints(selected)
	return selected
}

func contextSelectSplitCandidate(root *contextSemanticNode, maxDepth int) *contextSemanticNode {
	leaves := contextCollectLeafNodes(root)
	var best *contextSemanticNode
	for _, leaf := range leaves {
		if leaf.Locked {
			continue
		}
		if leaf.Depth >= maxDepth {
			continue
		}
		if len(leaf.FileIndices) < 6 {
			continue
		}
		if best == nil || len(leaf.FileIndices) > len(best.FileIndices) {
			best = leaf
		}
	}
	return best
}

func contextCollectLeafNodes(root *contextSemanticNode) []*contextSemanticNode {
	if root == nil {
		return nil
	}
	if len(root.Children) == 0 {
		return []*contextSemanticNode{root}
	}
	var out []*contextSemanticNode
	for _, child := range root.Children {
		out = append(out, contextCollectLeafNodes(child)...)
	}
	return out
}

func contextSpectralBisect(vectors [][]float64, indices []int) ([]int, []int, bool) {
	n := len(indices)
	if n < 4 {
		return nil, nil, false
	}

	sim := make([]float64, n*n)
	for i := 0; i < n; i++ {
		sim[i*n+i] = 1
		for j := i + 1; j < n; j++ {
			s := contextCosine(vectors[indices[i]], vectors[indices[j]])
			if s < 0 {
				s = 0
			}
			sim[i*n+j] = s
			sim[j*n+i] = s
		}
	}

	useSparse := n > 30
	neighbors := 12
	if n-1 < neighbors {
		neighbors = n - 1
	}
	adj := make([]bool, n*n)
	if useSparse {
		for i := 0; i < n; i++ {
			type pair struct {
				j int
				s float64
			}
			var scores []pair
			for j := 0; j < n; j++ {
				if i == j {
					continue
				}
				if sim[i*n+j] <= 0 {
					continue
				}
				scores = append(scores, pair{j: j, s: sim[i*n+j]})
			}
			sort.Slice(scores, func(a, b int) bool { return scores[a].s > scores[b].s })
			if len(scores) > neighbors {
				scores = scores[:neighbors]
			}
			for _, p := range scores {
				adj[i*n+p.j] = true
				adj[p.j*n+i] = true
			}
		}
	} else {
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				if sim[i*n+j] > 0 {
					adj[i*n+j] = true
					adj[j*n+i] = true
				}
			}
		}
	}

	w := make([]float64, n*n)
	degree := make([]float64, n)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if !adj[i*n+j] {
				continue
			}
			v := sim[i*n+j]
			w[i*n+j] = v
			w[j*n+i] = v
			degree[i] += v
			degree[j] += v
		}
	}
	for i := 0; i < n; i++ {
		if degree[i] == 0 {
			degree[i] = 1e-9
		}
	}

	lData := make([]float64, n*n)
	for i := 0; i < n; i++ {
		lData[i*n+i] = 1
		for j := i + 1; j < n; j++ {
			if w[i*n+j] <= 0 {
				continue
			}
			v := -w[i*n+j] / math.Sqrt(degree[i]*degree[j])
			lData[i*n+j] = v
			lData[j*n+i] = v
		}
	}
	lap := mat.NewSymDense(n, lData)
	var eig mat.EigenSym
	if ok := eig.Factorize(lap, true); !ok {
		return nil, nil, false
	}
	values := eig.Values(nil)
	if len(values) < 2 {
		return nil, nil, false
	}
	order := make([]int, len(values))
	for i := range values {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool { return values[order[i]] < values[order[j]] })
	fiedlerCol := order[1]

	vecs := mat.NewDense(n, n, nil)
	eig.VectorsTo(vecs)
	fiedler := make([]float64, n)
	for i := 0; i < n; i++ {
		fiedler[i] = vecs.At(i, fiedlerCol)
	}
	threshold := contextMedianFloat64(fiedler)

	var left, right []int
	for i := 0; i < n; i++ {
		if fiedler[i] <= threshold {
			left = append(left, indices[i])
		} else {
			right = append(right, indices[i])
		}
	}
	if len(left) < 2 || len(right) < 2 {
		left = nil
		right = nil
		for i := 0; i < n; i++ {
			if fiedler[i] <= 0 {
				left = append(left, indices[i])
			} else {
				right = append(right, indices[i])
			}
		}
	}
	if len(left) < 2 || len(right) < 2 {
		return nil, nil, false
	}
	return left, right, true
}

func contextMedianFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]float64(nil), values...)
	sort.Float64s(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return cp[mid]
	}
	return (cp[mid-1] + cp[mid]) / 2
}

func contextCentroidForIndices(vectors [][]float64, indices []int) []float64 {
	if len(indices) == 0 {
		return nil
	}
	dim := len(vectors[indices[0]])
	centroid := make([]float64, dim)
	for _, idx := range indices {
		v := vectors[idx]
		limit := dim
		if len(v) < limit {
			limit = len(v)
		}
		for i := 0; i < limit; i++ {
			centroid[i] += v[i]
		}
	}
	for i := range centroid {
		centroid[i] /= float64(len(indices))
	}
	return contextNormalizeVector(centroid)
}

func contextRenderSemanticNode(b *strings.Builder, node *contextSemanticNode, entries []contextFileEntry, depth int) {
	if node == nil {
		return
	}
	indent := strings.Repeat("  ", depth)
	files := make([]contextFileEntry, 0, len(node.FileIndices))
	for _, idx := range node.FileIndices {
		if idx >= 0 && idx < len(entries) {
			files = append(files, entries[idx])
		}
	}
	terms := contextTopTerms(files, 5)

	label := node.ID
	if len(terms) > 0 {
		show := 3
		if len(terms) < show {
			show = len(terms)
		}
		label = fmt.Sprintf("%s [%s]", node.ID, strings.Join(terms[:show], ", "))
	}

	b.WriteString(fmt.Sprintf("%s- Cluster %s (%d files)\n", indent, label, len(files)))
	if len(node.Children) == 0 {
		show := 6
		if len(files) < show {
			show = len(files)
		}
		for i := 0; i < show; i++ {
			b.WriteString(fmt.Sprintf("%s  - %s\n", indent, files[i].RelPath))
		}
		if len(files) > show {
			b.WriteString(fmt.Sprintf("%s  - ...\n", indent))
		}
		return
	}
	for _, child := range node.Children {
		contextRenderSemanticNode(b, child, entries, depth+1)
	}
}

func contextCollectHubs(root string) ([]contextHub, error) {
	var hubs []contextHub
	re := regexp.MustCompile(`\\[\\[([^\\]|#]+)[^\\]]*\\]\\]`)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if contextSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			if strings.HasPrefix(d.Name(), ".") && d.Name() != ".github" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		matches := re.FindAllStringSubmatch(string(data), -1)
		if len(matches) == 0 {
			return nil
		}
		links := make([]string, 0, len(matches))
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			links = append(links, strings.TrimSpace(m[1]))
		}
		hubs = append(hubs, contextHub{RelPath: contextRelativePath(root, path), AbsPath: path, Links: links})
		return nil
	})
	return hubs, err
}

func contextFindHubOrphans(root string, hubs []contextHub) []string {
	linked := map[string]bool{}
	for _, h := range hubs {
		for _, l := range h.Links {
			resolved := contextResolveHubLink(root, h.AbsPath, l)
			if resolved == "" {
				continue
			}
			linked[contextRelativePath(root, resolved)] = true
		}
	}

	entries, _ := contextCollectFiles(root, root)
	var orphans []string
	for _, e := range entries {
		if !linked[e.RelPath] {
			orphans = append(orphans, e.RelPath)
		}
	}
	sort.Strings(orphans)
	return orphans
}

func contextSelectHub(root string, hubs []contextHub, hubPath string, feature string) *contextHub {
	if strings.TrimSpace(hubPath) != "" {
		clean := filepath.Clean(strings.TrimPrefix(hubPath, "/"))
		for i := range hubs {
			if filepath.Clean(hubs[i].RelPath) == clean {
				return &hubs[i]
			}
		}
	}
	if strings.TrimSpace(feature) != "" {
		needle := strings.ToLower(feature)
		for i := range hubs {
			if strings.Contains(strings.ToLower(hubs[i].RelPath), needle) {
				return &hubs[i]
			}
		}
	}
	return nil
}

func contextResolveHubLink(root string, hubAbs string, link string) string {
	link = strings.TrimSpace(link)
	if link == "" {
		return ""
	}
	candidate := link
	if filepath.Ext(candidate) == "" {
		for _, ext := range []string{".ts", ".tsx", ".js", ".jsx", ".go", ".py", ".rs", ".md"} {
			if contextPathExists(filepath.Join(root, candidate+ext)) {
				candidate = candidate + ext
				break
			}
		}
	}

	if strings.HasPrefix(candidate, "/") {
		abs := filepath.Join(root, strings.TrimPrefix(candidate, "/"))
		if contextPathExists(abs) {
			return abs
		}
	}

	hubDir := filepath.Dir(hubAbs)
	local := filepath.Clean(filepath.Join(hubDir, candidate))
	if strings.HasPrefix(local, root) && contextPathExists(local) {
		return local
	}

	global := filepath.Clean(filepath.Join(root, candidate))
	if strings.HasPrefix(global, root) && contextPathExists(global) {
		return global
	}
	return ""
}

func contextPathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func contextValidateCommitContent(content string) string {
	var warnings []string
	lines := strings.Split(content, "\n")
	if len(lines) < 2 {
		warnings = append(warnings, "- Missing 2-line file header comment.")
	} else {
		first := strings.TrimSpace(lines[0])
		second := strings.TrimSpace(lines[1])
		if !contextIsCommentLine(first) || !contextIsCommentLine(second) {
			warnings = append(warnings, "- First two lines are not comments; Context+ style headers are recommended.")
		}
	}
	if len(lines) > 1000 {
		warnings = append(warnings, "- File exceeds 1000 lines; split into smaller modules.")
	}
	return strings.Join(warnings, "\n")
}

func contextIsCommentLine(line string) bool {
	return strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "/*") || strings.HasPrefix(line, "--")
}

func contextIndexPath(root string) string {
	return filepath.Join(contextDataDir(root), "index.json")
}

func contextEnsureIndex(root string) error {
	contextEnsureTracker(root)

	contextIndexMu.Lock()
	defer contextIndexMu.Unlock()

	idx, ok := contextIndexes[root]
	if !ok || idx == nil {
		idx = contextLoadIndex(root)
		contextIndexes[root] = idx
	}

	seen := map[string]bool{}
	var changed []string
	removed := false

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if contextSkipDirs[name] {
				return filepath.SkipDir
			}
			if strings.HasPrefix(name, ".") && name != ".github" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !contextCodeExt[ext] {
			return nil
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}

		rel := contextRelativePath(root, path)
		seen[rel] = true

		existing, exists := idx.Files[rel]
		mtime := info.ModTime().UnixMilli()
		size := info.Size()
		if exists && existing.MTime == mtime && existing.Size == size {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		content := string(data)
		if !contextLooksText(content) {
			return nil
		}

		snippet := content
		if len(snippet) > 2000 {
			snippet = snippet[:2000]
		}
		fileHash := contextHashString(content)
		idx.Files[rel] = contextIndexedFile{
			RelPath: rel,
			Ext:     ext,
			Lang:    contextLanguageByExt[ext],
			Header:  contextFileHeader(content),
			Snippet: snippet,
			Symbols: contextParseSymbolsByExt(ext, content),
			MTime:   mtime,
			Size:    size,
			Hash:    fileHash,
		}
		changed = append(changed, rel)
		return nil
	})
	if err != nil {
		return err
	}

	for rel := range idx.Files {
		if seen[rel] {
			continue
		}
		delete(idx.Files, rel)
		removed = true
	}

	if len(changed) > 0 || removed {
		idx.UpdatedAt = time.Now().UnixMilli()
		if saveErr := contextSaveIndex(root, idx); saveErr != nil {
			return saveErr
		}
	}

	if len(changed) > 0 {
		contextQueuePendingWarmLocked(root, changed)
	}

	return nil
}

func contextLoadIndex(root string) *contextProjectIndex {
	idx := &contextProjectIndex{
		Version:   1,
		Root:      root,
		UpdatedAt: time.Now().UnixMilli(),
		Files:     map[string]contextIndexedFile{},
	}
	data, err := os.ReadFile(contextIndexPath(root))
	if err != nil {
		return idx
	}
	var loaded contextProjectIndex
	if json.Unmarshal(data, &loaded) != nil {
		return idx
	}
	if loaded.Files == nil {
		loaded.Files = map[string]contextIndexedFile{}
	}
	loaded.Root = root
	if loaded.Version <= 0 {
		loaded.Version = 1
	}
	return &loaded
}

func contextSaveIndex(root string, idx *contextProjectIndex) error {
	if idx == nil {
		return nil
	}
	if err := os.MkdirAll(contextDataDir(root), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	return os.WriteFile(contextIndexPath(root), b, 0o644)
}

func contextEnsureTracker(root string) {
	if !contextEmbedTrackerEnabled() {
		return
	}
	contextIndexMu.Lock()
	if contextTrackerActive[root] {
		contextIndexMu.Unlock()
		return
	}
	contextTrackerActive[root] = true
	contextIndexMu.Unlock()
	go contextTrackerLoop(root)
}

func contextTrackerLoop(root string) {
	debounce := contextEnvInt("CONTEXTPLUS_EMBED_TRACKER_DEBOUNCE_MS", 700, 200, 10000)
	maxFiles := contextEnvInt("CONTEXTPLUS_EMBED_TRACKER_MAX_FILES", 8, 1, 20)
	ticker := time.NewTicker(time.Duration(debounce) * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		_ = contextEnsureIndex(root)
		files := contextPopPendingWarm(root, maxFiles)
		if len(files) == 0 {
			continue
		}

		contextIndexMu.Lock()
		idx := contextIndexes[root]
		indexedFiles := make([]contextIndexedFile, 0, len(files))
		for _, rel := range files {
			if f, ok := idx.Files[rel]; ok {
				indexedFiles = append(indexedFiles, f)
			}
		}
		contextIndexMu.Unlock()

		for _, f := range indexedFiles {
			contextWarmEmbeddingsForFile(root, f)
		}
	}
}

func contextQueuePendingWarmLocked(root string, relPaths []string) {
	set, ok := contextPendingWarm[root]
	if !ok {
		set = map[string]bool{}
		contextPendingWarm[root] = set
	}
	for _, rel := range relPaths {
		set[rel] = true
	}
}

func contextPopPendingWarm(root string, max int) []string {
	contextIndexMu.Lock()
	defer contextIndexMu.Unlock()
	set, ok := contextPendingWarm[root]
	if !ok || len(set) == 0 {
		return nil
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > max {
		keys = keys[:max]
	}
	for _, k := range keys {
		delete(set, k)
	}
	return keys
}

func contextWarmEmbeddingsForFile(root string, file contextIndexedFile) {
	fileDoc := strings.ToLower(strings.Join([]string{
		file.RelPath,
		file.Header,
		contextSymbolsToText(file.Symbols, 16),
		file.Snippet,
	}, "\n"))
	if strings.TrimSpace(fileDoc) != "" {
		_, _ = contextEmbedding(root, fmt.Sprintf("file:%s:%s:%s", file.RelPath, file.Hash, fileDoc))
	}

	maxIdentifiers := 16
	if len(file.Symbols) < maxIdentifiers {
		maxIdentifiers = len(file.Symbols)
	}
	for i := 0; i < maxIdentifiers; i++ {
		s := file.Symbols[i]
		idDoc := strings.ToLower(fmt.Sprintf("%s %s %s %s %s", s.Name, s.Kind, s.Signature, file.RelPath, file.Header))
		_, _ = contextEmbedding(root, fmt.Sprintf("identifier:%s:%d:%s:%s", file.RelPath, s.Line, file.Hash, idDoc))
	}
}

func contextSymbolsToText(symbols []contextSymbol, limit int) string {
	if len(symbols) == 0 || limit <= 0 {
		return ""
	}
	if len(symbols) < limit {
		limit = len(symbols)
	}
	parts := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		parts = append(parts, symbols[i].Name)
	}
	return strings.Join(parts, " ")
}

func contextEmbedTrackerEnabled() bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("CONTEXTPLUS_EMBED_TRACKER")))
	if raw == "" {
		return true
	}
	return raw != "0" && raw != "false" && raw != "no" && raw != "off"
}

func contextEnvInt(name string, def int, min int, max int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

func contextPathWithinTarget(path string, target string) bool {
	cleanPath := filepath.Clean(path)
	cleanTarget := filepath.Clean(target)
	if cleanPath == cleanTarget {
		return true
	}
	return strings.HasPrefix(cleanPath, cleanTarget+string(os.PathSeparator))
}

func contextGetIndexedFile(root string, relPath string) (contextIndexedFile, bool) {
	contextIndexMu.Lock()
	defer contextIndexMu.Unlock()
	idx, ok := contextIndexes[root]
	if !ok || idx == nil {
		return contextIndexedFile{}, false
	}
	f, ok := idx.Files[filepath.Clean(relPath)]
	return f, ok
}

func contextHashString(content string) string {
	sum := sha1.Sum([]byte(content))
	return hex.EncodeToString(sum[:])
}

func contextLineByNumber(content string, lineNumber int) string {
	if lineNumber <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if lineNumber > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[lineNumber-1])
}

func contextDataDir(root string) string {
	return filepath.Join(root, ".apollo_contextplus")
}

func contextRestoreBaseDir(root string) string {
	return filepath.Join(contextDataDir(root), "restore_points")
}

func contextCreateRestorePoint(root string, relPaths []string, message string) (string, error) {
	if err := os.MkdirAll(contextRestoreBaseDir(root), 0o755); err != nil {
		return "", err
	}

	random := make([]byte, 3)
	_, _ = rand.Read(random)
	rpID := fmt.Sprintf("rp-%d-%s", time.Now().UnixMilli(), hex.EncodeToString(random))
	rpDir := filepath.Join(contextRestoreBaseDir(root), rpID)
	filesDir := filepath.Join(rpDir, "files")
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		return "", err
	}

	point := contextRestorePoint{
		ID:        rpID,
		Timestamp: time.Now().UnixMilli(),
		Message:   message,
		Files:     make([]contextRestorePointFile, 0, len(relPaths)),
	}

	for _, rel := range relPaths {
		rel = filepath.Clean(strings.TrimPrefix(rel, "/"))
		abs := filepath.Join(root, rel)
		fileEntry := contextRestorePointFile{Path: rel}
		if contextPathExists(abs) {
			fileEntry.Existed = true
			backupPath := filepath.Join(filesDir, rel)
			if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
				return "", err
			}
			src, err := os.Open(abs)
			if err != nil {
				return "", err
			}
			dst, err := os.Create(backupPath)
			if err != nil {
				src.Close()
				return "", err
			}
			_, copyErr := io.Copy(dst, src)
			src.Close()
			dst.Close()
			if copyErr != nil {
				return "", copyErr
			}
		}
		point.Files = append(point.Files, fileEntry)
	}

	metaBytes, _ := json.MarshalIndent(point, "", "  ")
	if err := os.WriteFile(filepath.Join(rpDir, "meta.json"), metaBytes, 0o644); err != nil {
		return "", err
	}
	return rpID, nil
}

func contextListRestorePoints(root string) ([]contextRestorePoint, error) {
	base := contextRestoreBaseDir(root)
	if !contextPathExists(base) {
		return []contextRestorePoint{}, nil
	}
	dirs, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	var points []contextRestorePoint
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		metaPath := filepath.Join(base, d.Name(), "meta.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var p contextRestorePoint
		if json.Unmarshal(data, &p) != nil {
			continue
		}
		points = append(points, p)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Timestamp > points[j].Timestamp })
	return points, nil
}

func contextRestorePointByID(root string, pointID string) ([]string, error) {
	metaPath := filepath.Join(contextRestoreBaseDir(root), pointID, "meta.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("restore point not found")
	}
	var p contextRestorePoint
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}

	filesDir := filepath.Join(contextRestoreBaseDir(root), pointID, "files")
	var restored []string
	for _, f := range p.Files {
		abs := filepath.Join(root, f.Path)
		if !strings.HasPrefix(abs, root) {
			continue
		}
		if f.Existed {
			backup := filepath.Join(filesDir, f.Path)
			if !contextPathExists(backup) {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return restored, err
			}
			src, err := os.Open(backup)
			if err != nil {
				return restored, err
			}
			dst, err := os.Create(abs)
			if err != nil {
				src.Close()
				return restored, err
			}
			_, copyErr := io.Copy(dst, src)
			src.Close()
			dst.Close()
			if copyErr != nil {
				return restored, copyErr
			}
			restored = append(restored, f.Path)
		} else {
			_ = os.Remove(abs)
			restored = append(restored, f.Path)
		}
	}
	return restored, nil
}

func contextRelativePath(root string, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	if rel == "." {
		return rel
	}
	return filepath.Clean(rel)
}

func contextDetectLanguages(target string) map[string]bool {
	langs := map[string]bool{}
	_ = filepath.WalkDir(target, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if contextSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			if strings.HasPrefix(d.Name(), ".") && d.Name() != ".github" {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".go":
			langs["go"] = true
		case ".ts", ".tsx":
			langs["typescript"] = true
		case ".js", ".jsx":
			langs["javascript"] = true
		case ".py":
			langs["python"] = true
		case ".rs":
			langs["rust"] = true
		}
		return nil
	})
	return langs
}

func contextFindFilesByExt(root string, ext string, limit int) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if contextSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			if strings.HasPrefix(d.Name(), ".") && d.Name() != ".github" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ext) {
			out = append(out, path)
			if len(out) >= limit {
				return io.EOF
			}
		}
		return nil
	})
	return out
}

func contextRunCheckCommand(dir string, title string, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Sprintf("[%s]\nTimed out after 2m", title)
	}
	if err != nil {
		if output == "" {
			return fmt.Sprintf("[%s]\nFailed: %v", title, err)
		}
		return fmt.Sprintf("[%s]\n%s", title, output)
	}
	if output == "" {
		return fmt.Sprintf("[%s]\n✓ No issues", title)
	}
	return fmt.Sprintf("[%s]\n%s", title, output)
}

func contextFindIntArg(raw map[string]interface{}, key string, def int) int {
	v, ok := raw[key]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		if n, err := strconv.Atoi(t); err == nil {
			return n
		}
	}
	return def
}
