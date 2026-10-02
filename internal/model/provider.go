package model

// StoredProviderSettings crosses the metadata/store seam with ciphertext only.
// CatalogID is a stable directory-space identity, not a credential version.
type StoredProviderSettings struct {
	Source, CatalogID, BaseURL string
	Secrets                    []byte
	Revision                   int64
}
