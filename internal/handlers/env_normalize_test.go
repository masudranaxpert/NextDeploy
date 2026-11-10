package handlers

import "testing"

func TestNormalizeEnvContent(t *testing.T) {
	in := "DB_DATABASE=costcount_ecom2\t \r\n" +
		"DB_HOST=\tnd-mysql\t\r\n" +
		"APP_NAME=\"My App \"\n" +
		"RAW_PASS='keep\ttab'\n" +
		"SPACED=hello world \n" +
		"# comment line\t\n"
	want := "DB_DATABASE=costcount_ecom2\n" +
		"DB_HOST=nd-mysql\n" +
		"APP_NAME=\"My App \"\n" +
		"RAW_PASS='keep\ttab'\n" +
		"SPACED=hello world\n" +
		"# comment line\t\n"

	got := normalizeEnvContent(in)
	if got != want {
		t.Fatalf("normalizeEnvContent mismatch:\ngot:\n%q\nwant:\n%q", got, want)
	}
}
