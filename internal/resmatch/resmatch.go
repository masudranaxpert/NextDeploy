package resmatch

import "strings"

func TrimResourceName(s string) string {
	return strings.TrimPrefix(strings.TrimSpace(s), "/")
}

func LongestMatchingProject(name string, candidates []string, delim string) string {
	name = TrimResourceName(name)
	best := ""
	for _, proj := range candidates {
		proj = strings.TrimSpace(proj)
		if proj == "" {
			continue
		}
		if name == proj || strings.HasPrefix(name, proj+delim) {
			if len(proj) > len(best) {
				best = proj
			}
		}
	}
	return best
}

func OwnedByProject(name, project, delim string, allCandidates []string) bool {
	project = strings.TrimSpace(project)
	name = TrimResourceName(name)
	if project == "" || name == "" {
		return false
	}
	if name == project {
		return true
	}
	if !strings.HasPrefix(name, project+delim) {
		return false
	}
	if len(allCandidates) == 0 {
		return false
	}
	return LongestMatchingProject(name, allCandidates, delim) == project
}

func MatchesVolumeName(vol, project string) bool {
	project = strings.TrimSpace(project)
	if project == "" {
		return false
	}
	alt := strings.ReplaceAll(project, "-", "_")
	return vol == project || vol == alt ||
		strings.HasPrefix(vol, project+"_") || strings.HasPrefix(vol, alt+"_")
}

func MatchesComposeContainerName(name, project string, allProjects []string) bool {
	if OwnedByProject(name, project, "-", allProjects) {
		return true
	}
	return OwnedByProject(name, project, "_", allProjects)
}

func MatchesImageRepo(repo, project string, allProjects []string) bool {
	repo = strings.TrimSpace(repo)
	project = strings.TrimSpace(project)
	if repo == "" || project == "" {
		return false
	}
	alt := strings.ReplaceAll(project, "-", "_")
	if repo == project || repo == alt {
		return true
	}
	return OwnedByProject(repo, project, "_", allProjects) || OwnedByProject(repo, alt, "_", allProjects)
}
