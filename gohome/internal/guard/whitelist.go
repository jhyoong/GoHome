package guard

// WhitelistFile is the on-disk representation of a whitelist JSON file.
// Tools lists exact tool names that are always allowed.
// Shell lists shell command regex patterns (anchored automatically at load time).
type WhitelistFile struct {
	Tools []string `json:"tools"`
	Shell []string `json:"shell"`
}
