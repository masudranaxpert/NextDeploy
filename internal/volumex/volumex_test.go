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

func TestIsInfraBackupVolumeName(t *testing.T) {
	infra := []string{
		"nextdeploy_caddy_data",
		"nextdeploy_caddy_config",
		"letsencrypt_certs",
		"nextdeploy_angie_acme",
		"nextdeploy_ferron_acme",
		"nd-proxy-state",
		"nextdeploy_nd_proxy",
		"nd_pki",
		"nd-pki-certs",
	}
	for _, name := range infra {
		if !isInfraBackupVolumeName(name) {
			t.Errorf("isInfraBackupVolumeName(%q) = false, want true", name)
		}
	}

	appVols := []string{
		"blog_db_data",
		"shop_uploads",
		"api_cache",
	}
	for _, name := range appVols {
		if isInfraBackupVolumeName(name) {
			t.Errorf("isInfraBackupVolumeName(%q) = true, want false", name)
		}
	}
}
