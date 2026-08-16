---
description: React performance — memo, useMemo, useCallback, virtualization, ref patterns, common pitfalls
---

# React Patterns Skill

## Memoization Decision Tree
1. Is the component re-rendering too often? → `React.memo(Component)`
2. Is an expensive calculation running every render? → `useMemo`
3. Is a callback being re-created every render and passed to a child? → `useCallback`
4. Do you need to access a DOM node or preserve a value without re-rendering? → `useRef`

## React.memo
```jsx
const ExpensiveList = React.memo(({ items, onSelect }) => {
  return items.map(item => <Item key={item.id} item={item} onClick={onSelect} />);
});
// Only re-renders when items or onSelect reference changes
```

## useMemo — expensive transforms
```jsx
const sorted = useMemo(() =>
  [...items].sort((a, b) => a.name.localeCompare(b.name)),
  [items] // recalculate only when items changes
);
```

## useCallback — stable handlers
```jsx
const handleClick = useCallback((id) => {
  setSelected(id);
}, []); // stable if no dependencies
```

## useRef Patterns
```jsx
// DOM access
const inputRef = useRef(null);
useEffect(() => { inputRef.current?.focus(); }, []);

// Mutable value without triggering re-render (e.g. abort controller, timer)
const abortRef = useRef(null);
abortRef.current = new AbortController();
```

## Virtualization (large lists)
Use `react-window` or `@tanstack/react-virtual` for lists > 200 items:
```jsx
import { FixedSizeList } from 'react-window';
<FixedSizeList height={500} itemCount={items.length} itemSize={40}>
  {({ index, style }) => <div style={style}>{items[index].name}</div>}
</FixedSizeList>
```

## Context Performance
Context re-renders ALL consumers when value changes.  
Split contexts: `<UserContext>` + `<ThemeContext>` rather than one giant `<AppContext>`.  
Use `useMemo` for context values:
```jsx
const value = useMemo(() => ({ user, setUser }), [user]);
<UserContext.Provider value={value}>
```

## State Update Pitfalls
```jsx
// WRONG: stale closure
setCount(count + 1); setCount(count + 1); // only increments by 1

// CORRECT: updater function
setCount(c => c + 1); setCount(c => c + 1); // increments by 2
```

## Cleanup Pattern
```jsx
useEffect(() => {
  const controller = new AbortController();
  fetch('/api/data', { signal: controller.signal }).then(setData);
  return () => controller.abort(); // cleanup on unmount
}, []);
```

## Key Prop Rules
- Always use stable unique IDs — never array index for dynamic/reorderable lists
- Changing `key` forces full remount (useful to reset a component's state intentionally)
