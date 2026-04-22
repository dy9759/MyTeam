package cli

import "testing"

func TestResolveReleaseRepoDefaultsToMyTeam(t *testing.T) {
	t.Setenv("MYTEAM_RELEASE_REPO", "")

	if got := resolveReleaseRepo(); got != defaultReleaseRepo {
		t.Fatalf("resolveReleaseRepo() = %q, want %q", got, defaultReleaseRepo)
	}
	if got := releaseListAPIURL(); got != "https://api.github.com/repos/MyAIOSHub/MyTeam/releases?per_page=20" {
		t.Fatalf("releaseListAPIURL() = %q", got)
	}
}

func TestResolveReleaseRepoSupportsOverride(t *testing.T) {
	t.Setenv("MYTEAM_RELEASE_REPO", "MyAIOSHub/MyTeam")

	if got := resolveReleaseRepo(); got != "MyAIOSHub/MyTeam" {
		t.Fatalf("resolveReleaseRepo() = %q", got)
	}
	if got := releaseListAPIURL(); got != "https://api.github.com/repos/MyAIOSHub/MyTeam/releases?per_page=20" {
		t.Fatalf("releaseListAPIURL() = %q", got)
	}
	if got := releaseDownloadURL("v1.2.3", "myteam_darwin_arm64.tar.gz"); got != "https://github.com/MyAIOSHub/MyTeam/releases/download/v1.2.3/myteam_darwin_arm64.tar.gz" {
		t.Fatalf("releaseDownloadURL() = %q", got)
	}
}

func TestResolveBrewFormulaSupportsOverride(t *testing.T) {
	t.Setenv("MYTEAM_BREW_FORMULA", "MyAIOSHub/tap/myteam")

	if got := resolveBrewFormula(); got != "MyAIOSHub/tap/myteam" {
		t.Fatalf("resolveBrewFormula() = %q", got)
	}
}

func TestResolveGitHubTokenPrefersMyTeamToken(t *testing.T) {
	t.Setenv("MYTEAM_GITHUB_TOKEN", "myteam-token")
	t.Setenv("GITHUB_TOKEN", "github-token")

	if got := resolveGitHubToken(); got != "myteam-token" {
		t.Fatalf("resolveGitHubToken() = %q", got)
	}
}

func TestResolveGitHubTokenFallsBackToGitHubToken(t *testing.T) {
	t.Setenv("MYTEAM_GITHUB_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "github-token")

	if got := resolveGitHubToken(); got != "github-token" {
		t.Fatalf("resolveGitHubToken() = %q", got)
	}
}

func TestSelectPreferredReleasePrefersStable(t *testing.T) {
	releases := []GitHubRelease{
		{TagName: "v0.2.0-beta.2", Prerelease: true},
		{TagName: "v0.1.9", Prerelease: false},
	}

	got := selectPreferredRelease(releases)
	if got == nil || got.TagName != "v0.1.9" {
		t.Fatalf("selectPreferredRelease() = %#v", got)
	}
}

func TestSelectPreferredReleaseFallsBackToBeta(t *testing.T) {
	releases := []GitHubRelease{
		{TagName: "v0.2.0-beta.2", Prerelease: true},
		{TagName: "v0.2.0-beta.1", Prerelease: true},
	}

	got := selectPreferredRelease(releases)
	if got == nil || got.TagName != "v0.2.0-beta.2" {
		t.Fatalf("selectPreferredRelease() = %#v", got)
	}
}

func TestSelectPreferredReleaseSkipsDrafts(t *testing.T) {
	releases := []GitHubRelease{
		{TagName: "v0.2.0-beta.3", Prerelease: true, Draft: true},
		{TagName: "v0.2.0-beta.2", Prerelease: true},
	}

	got := selectPreferredRelease(releases)
	if got == nil || got.TagName != "v0.2.0-beta.2" {
		t.Fatalf("selectPreferredRelease() = %#v", got)
	}
}
