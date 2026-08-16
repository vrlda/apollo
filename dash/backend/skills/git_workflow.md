---
description: Git workflows — branching strategy, interactive rebase, cherry-pick, bisect, stash, undo recipes
---

# Git Workflow Skill

## Branch Strategy
```
main          — always deployable
dev           — integration branch
feat/NAME     — feature branches (from dev)
fix/NAME      — bug fixes
hotfix/NAME   — emergency patches directly from main
```

## Interactive Rebase (squash, reorder, edit)
```bash
git rebase -i HEAD~5          # rebase last 5 commits
# In editor: pick → squash (s) to merge, reword (r) to edit message
git rebase -i main            # squash entire feature branch for clean merge
```

## Cherry-Pick
```bash
git cherry-pick abc1234                    # apply single commit
git cherry-pick abc1234..def5678          # apply range of commits
git cherry-pick -n abc1234                 # stage without committing (--no-commit)
```

## Bisect (find the commit that introduced a bug)
```bash
git bisect start
git bisect bad                    # current commit is broken
git bisect good v2.1.0            # last known good tag
# git does binary search — test each checkout:
git bisect good  # or: git bisect bad
git bisect reset  # when done
```

## Stash
```bash
git stash                         # stash all tracked changes
git stash -u                      # include untracked files
git stash push -m "WIP: auth"     # named stash
git stash list
git stash pop                     # apply + remove latest
git stash apply stash@{2}         # apply specific, keep in list
```

## Undo Recipes
```bash
# Undo last commit, keep changes staged
git reset --soft HEAD~1

# Undo last commit, unstage changes (default)
git reset HEAD~1

# Undo last commit, DISCARD changes (dangerous)
git reset --hard HEAD~1

# Revert a specific commit (safe, creates new commit)
git revert abc1234

# Unstage a file
git restore --staged file.go

# Discard working copy changes in a file
git restore file.go
```

## Rebase vs Merge
| Situation | Use |
|---|---|
| Feature branch → main (clean history) | `rebase` then fast-forward merge |
| Hotfix with exact timestamp preservation | `merge --no-ff` |
| Public branch (others have checked out) | Always `merge` — never rebase public history |

## Useful Aliases
```bash
git config --global alias.lg "log --oneline --graph --decorate --all"
git config --global alias.st "status -sb"
git config --global alias.undo "reset HEAD~1 --mixed"
```

## Conventional Commits
```
feat: add user authentication
fix: resolve null pointer in order processing
docs: update API documentation
refactor: extract payment service
perf: optimize database query caching
chore: upgrade Go to 1.24
```
