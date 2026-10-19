package integrations

// MaskedSecret replaces a stored secret in API responses. Clients must treat
// this value (and an empty string) as "unchanged" when saving configuration.
const MaskedSecret = "••••"

// MaskSecret masks a non-empty secret for display.
func MaskSecret(v string) string {
	if v == "" {
		return ""
	}
	return MaskedSecret
}

// secretUnchanged reports whether a submitted value means "keep the stored
// secret". Blank fields and the mask placeholder both preserve the old value.
func secretUnchanged(v string) bool {
	return v == "" || v == MaskedSecret
}

// existingSecret loads a single-column secret from a settings row. Any lookup
// error yields an empty string so callers fall back to the submitted value.
func (s *Service) existingSecret(query string) string {
	var v string
	_ = s.db.QueryRow(query).Scan(&v)
	return v
}
