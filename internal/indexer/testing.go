package indexer

// GetWriter returns the internal writer from an Indexer for testing purposes.
// This should only be used in tests.
// Returns nil if using non-JSON storage backend.
func GetWriter(idx Indexer) *AtomicWriter {
	if impl, ok := idx.(*indexer); ok {
		return impl.getWriter()
	}
	return nil
}
