package settings

import "testing"

// FuzzSettingsParse feeds arbitrary strings to the settings tag tokenizer
// and parser. These split a `setting:"…"` tag body into key=value tokens.
// The parser must never panic on malformed input — it must return an error.
//
// (The on-disk TOML loader, loadTOMLValues, is intentionally not fuzzed here:
// it is hard-tied to toml.DecodeFile reading a path from disk, so it has no
// pure string/bytes entry point to drive without I/O.)
func FuzzSettingsParse(f *testing.F) {
	f.Add("name=foo,type=string,desc=hello")
	f.Add("name=foo,type=int,min=0,max=10,desc='a number'")
	f.Add("name=e,type=enum,values=a|b|c,desc='pick one'")
	f.Add("")
	f.Add("noequalssign")
	f.Add("desc='unterminated quote")
	f.Add("name=x,,type=bool,desc=y")
	f.Add("'")
	f.Add("=emptykey")
	f.Add("name=x,type=string,desc='comma, inside, quotes'")

	f.Fuzz(func(t *testing.T, raw string) {
		// tokenizeTag must never panic on arbitrary input.
		_, _ = tokenizeTag(raw)
		// parseTag (which calls tokenizeTag) must also never panic; on
		// malformed tags it is expected to return an error rather than a
		// partially-populated FieldMeta.
		_, _ = parseTag(raw)
	})
}
