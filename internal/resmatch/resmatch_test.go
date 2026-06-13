package resmatch

import "testing"

func TestMatchesComposeContainerSiblingSlug(t *testing.T) {
	all := []string{"diu", "diu-test"}
	if MatchesComposeContainerName("diu-test_tracker-db", "diu", all) {
		t.Fatal("diu must not own diu-test container")
	}
	if !MatchesComposeContainerName("diu-test_tracker-db", "diu-test", all) {
		t.Fatal("diu-test must own its container")
	}
	if !MatchesComposeContainerName("diu-web-1", "diu", all) {
		t.Fatal("diu must own diu-web-1")
	}
}

func TestMatchesVolumeNameSiblingSlug(t *testing.T) {
	if MatchesVolumeName("diu-test_db_data", "diu") {
		t.Fatal("diu must not own diu-test volume name")
	}
	if !MatchesVolumeName("diu-test_db_data", "diu-test") {
		t.Fatal("diu-test must own its volume name")
	}
}
