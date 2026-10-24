package volumex

import (
	"testing"

	"panel/internal/dockerapi"
)

func TestMountRefMatchesAppWorkspace(t *testing.T) {
	q := AppVolumeQuery{
		AppID:         "ispbilling",
		WorkspaceRoot: "/data/workspaces/ispbilling",
		ComposeProjects: []string{"ispbilling"},
	}
	ref := dockerapi.VolumeMountRef{
		VolumeName:     "25df492f2ecc120a2cc7dd7622612988c27fc086e419715c150870c42986d950",
		ContainerName:  "ispbilling-redis-1",
		ComposeProject: "ispbilling",
		WorkingDir:     "/data/workspaces/ispbilling",
	}
	if !mountRefMatchesApp(ref, q) {
		t.Fatal("anonymous redis volume should match app via workspace")
	}
}

func TestMountRefRejectsSiblingApp(t *testing.T) {
	all := []string{"diu", "diu-test"}
	q := AppVolumeQuery{
		AppID:            "diu",
		WorkspaceRoot:    "/data/workspaces/diu",
		ComposeProjects:  []string{"diu"},
		AllPanelProjects: all,
	}
	ref := dockerapi.VolumeMountRef{
		VolumeName:     "diu-test_db_data",
		ContainerName:  "diu-test_tracker-db",
		ComposeProject: "diu-test",
		WorkingDir:     "/data/workspaces/diu-test",
	}
	if mountRefMatchesApp(ref, q) {
		t.Fatal("diu app must not claim diu-test volume")
	}
}

func TestHybridContainerIndexPerApp(t *testing.T) {
	index := dockerapi.NewVolumeMountIndex(map[string][]dockerapi.VolumeMountRef{
			"diu-test_db_data": {{
				VolumeName:     "diu-test_db_data",
				ContainerName:  "diu-test_tracker-db",
				ComposeProject: "diu-test",
				WorkingDir:     "/data/workspaces/diu-test",
			}},
			"diu_db_data": {{
				VolumeName:     "diu_db_data",
				ContainerName:  "diu_web",
				ComposeProject: "diu",
				WorkingDir:     "/data/workspaces/diu",
			}},
	})
	all := []string{"diu", "diu-test"}
	diuQ := AppVolumeQuery{
		AppID: "diu", WorkspaceRoot: "/data/workspaces/diu",
		ComposeProjects: []string{"diu"}, AllPanelProjects: all,
	}
	diuTestQ := AppVolumeQuery{
		AppID: "diu-test", WorkspaceRoot: "/data/workspaces/diu-test",
		ComposeProjects: []string{"diu-test"}, AllPanelProjects: all,
	}
	diuVols := volumesFromContainerIndex(index, diuQ)
	if len(diuVols) != 1 || diuVols[0] != "diu_db_data" {
		t.Fatalf("diu volumes = %v", diuVols)
	}
	diuTestVols := volumesFromContainerIndex(index, diuTestQ)
	if len(diuTestVols) != 1 || diuTestVols[0] != "diu-test_db_data" {
		t.Fatalf("diu-test volumes = %v", diuTestVols)
	}
}

func TestMatchAppIDForVolumeAnonymous(t *testing.T) {
	index := dockerapi.NewVolumeMountIndex(map[string][]dockerapi.VolumeMountRef{
			"hashvol": {{
				VolumeName:     "hashvol",
				ContainerName:  "ispbilling-redis-1",
				ComposeProject: "ispbilling",
				WorkingDir:     "/data/workspaces/ispbilling",
			}},
	})
	queries := []AppVolumeQuery{{
		AppID: "ispbilling", WorkspaceRoot: "/data/workspaces/ispbilling",
		ComposeProjects: []string{"ispbilling"},
	}}
	m := &Matcher{index: index}
	if got := m.MatchAppIDForVolume("hashvol", queries); got != "ispbilling" {
		t.Fatalf("owner = %q, want ispbilling", got)
	}
}
