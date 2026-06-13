package volumex

import "testing"

func TestFilterForAppSiblingSlug(t *testing.T) {
	all := []string{"diu_db_data", "diu-test_db_data", "diu-test_backend_uploads", "other_data"}
	diu := FilterForApp("diu", all, []string{"diu"})
	if len(diu) != 1 || diu[0] != "diu_db_data" {
		t.Fatalf("FilterForApp(diu) = %v, want [diu_db_data]", diu)
	}
	diuTest := FilterForApp("diu-test", all, []string{"diu-test"})
	want := []string{"diu-test_backend_uploads", "diu-test_db_data"}
	if len(diuTest) != len(want) {
		t.Fatalf("FilterForApp(diu-test) = %v, want %v", diuTest, want)
	}
}
