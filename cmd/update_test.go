package cmd

import "testing"

func TestSelectReleaseAssetMatchesVersionedLinuxAMD64(t *testing.T) {
	release := ghRelease{
		TagName: "v2.3.5",
		Assets: []ghAsset{
			{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums.txt"},
			{Name: "chb_2.3.5_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.com/chb_2.3.5_linux_amd64.tar.gz"},
			{Name: "chb_2.3.5_linux_arm64.tar.gz", BrowserDownloadURL: "https://example.com/chb_2.3.5_linux_arm64.tar.gz"},
		},
	}

	asset, ok := selectReleaseAsset(release, "linux", "amd64")
	if !ok {
		t.Fatal("expected linux/amd64 asset to be found")
	}
	if asset.Name != "chb_2.3.5_linux_amd64.tar.gz" {
		t.Fatalf("unexpected asset: %q", asset.Name)
	}
}

func TestSelectReleaseAssetFallsBackToUnversionedName(t *testing.T) {
	release := ghRelease{
		TagName: "v2.3.5",
		Assets: []ghAsset{
			{Name: "chb_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.com/chb_linux_amd64.tar.gz"},
		},
	}

	asset, ok := selectReleaseAsset(release, "linux", "amd64")
	if !ok {
		t.Fatal("expected fallback linux/amd64 asset to be found")
	}
	if asset.Name != "chb_linux_amd64.tar.gz" {
		t.Fatalf("unexpected fallback asset: %q", asset.Name)
	}
}

func TestReleaseAssetBinaryName(t *testing.T) {
	got := releaseAssetBinaryName("chb_2.3.5_linux_amd64.tar.gz")
	if got != "chb_2.3.5_linux_amd64" {
		t.Fatalf("unexpected binary name: %q", got)
	}
}
