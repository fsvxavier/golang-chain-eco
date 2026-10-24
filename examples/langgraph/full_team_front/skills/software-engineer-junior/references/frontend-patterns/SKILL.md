---
name: frontend-patterns
description: Frontend development patterns for React, Next.js, state management, performance optimization, and UI best practices. Use when building or reviewing React or Next.js components, state, or render performance.
metadata:
  origin: ECC
---

# Frontend Development Patterns

Patterns for React, Next.js and performant user interfaces. Use when building
components, managing state, fetching data, handling forms or tuning rendering.

## Component patterns

- Composition over inheritance: small parts (`Card`, `CardHeader`, `CardBody`)
  assembled by the caller, with `children` and a `variant` prop.
- Compound components share state through a context. The sub-component throws
  a clear error when used outside its parent:

```typescript
const ctx = useContext(TabsContext)
if (!ctx) throw new Error('Tab must be used within Tabs')
```

- Render props (`children` as a function) expose loading, error and data so
  the caller decides how each state looks.

## Custom hooks

- Wrap reusable state in a hook (`useToggle`, `useDebounce`).
- A data hook returns `{ data, error, loading, refetch }`. Keep the latest
  fetcher and options in refs so `refetch` stays referentially stable;
  otherwise inline functions cause an infinite fetch loop.
- Debounce with `setTimeout` inside an effect and clear it in the cleanup.

## State management

- Local state first (`useState`). Use `useReducer` plus context when several
  fields change together, with a typed action union and a provider hook that
  throws when used outside its provider.
- Derive values during render instead of mirroring them in state.

## Performance

- `useMemo` for expensive computations; copy before sorting
  (`[...items].sort(...)`) because `sort` mutates.
- `useCallback` for functions passed to memoized children; `React.memo` for
  pure components.
- `lazy` plus `Suspense` with a skeleton fallback for heavy components.
- Virtualize long lists (`@tanstack/react-virtual`) instead of rendering
  thousands of rows.

## Forms

- Controlled inputs with a typed `FormData` and a `FormErrors` object.
- `validate()` fills the errors and returns a boolean; submit calls
  `e.preventDefault()`, stops when invalid, and handles the failure of the
  request visibly.
- Show each error next to its field, and tie it to the input with
  `aria-describedby`.

## Error boundaries

A class component with `getDerivedStateFromError` and `componentDidCatch`
renders a fallback with the message and a "Try again" button that resets the
state. Wrap each screen, not only the app root.

## Animation

Use `AnimatePresence` plus `motion` for list and modal enter/exit with short
durations (about 0.3s). Respect `prefers-reduced-motion`.

## Accessibility

- Custom widgets are keyboard-operable: ArrowUp/ArrowDown move, Enter
  selects, Escape closes; expose `role`, `aria-expanded`, `aria-haspopup`.
- Dialogs use `role="dialog"`, `aria-modal="true"`, `tabIndex={-1}`, move
  focus in when opened, close on Escape and restore the previously focused
  element on close.
