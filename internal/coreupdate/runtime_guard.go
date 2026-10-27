package coreupdate

// RuntimeGuard covers both the earlier single-binary experiment and complete
// package transactions. Neither may let another operation run on a partial tree.
func RuntimeGuard(root string) error {
	if err := BundleStartupGuard(root); err != nil {
		return err
	}
	return StartupGuard(root)
}
