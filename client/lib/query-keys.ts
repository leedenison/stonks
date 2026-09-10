// Query keys, in one place so invalidation and the queries it targets cannot
// drift apart. The first element is the resource name, so invalidateQueries
// prefix-matches every variant of it, and every parameter is a primitive,
// because keys are compared structurally and a rebuilt object would be a new
// key that silently refetches.
export const qk = {
  session: () => ["session"] as const,
};
