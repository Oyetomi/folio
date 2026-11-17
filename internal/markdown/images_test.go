package markdown

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Oyetomi/folio/internal/config"
)

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stagedContent(t *testing.T, cfg *config.BookConfig, name string) string {
	t.Helper()
	if name == "" {
		t.Fatal("image was not staged")
	}
	b, err := os.ReadFile(filepath.Join(cfg.WorkDir, name))
	if err != nil {
		t.Fatalf("staged file %q: %v", name, err)
	}
	return string(b)
}

func newImageCfg(t *testing.T) (*config.BookConfig, string) {
	root := t.TempDir()
	return &config.BookConfig{RootDir: root, WorkDir: t.TempDir()}, root
}

func TestResolveImageRelativeToChapter(t *testing.T) {
	cfg, root := newImageCfg(t)
	writeFile(t, filepath.Join(root, "a", "img", "x.png"), "A")
	writeFile(t, filepath.Join(root, "b", "img", "x.png"), "B")
	writeFile(t, filepath.Join(root, "a", "ch.md"), "")
	writeFile(t, filepath.Join(root, "b", "ch.md"), "")

	nameA := resolveAndStageImage("img/x.png", cfg, filepath.Join(root, "a", "ch.md"))
	nameB := resolveAndStageImage("img/x.png", cfg, filepath.Join(root, "b", "ch.md"))
	if got := stagedContent(t, cfg, nameA); got != "A" {
		t.Errorf("chapter a got image %q", got)
	}
	if got := stagedContent(t, cfg, nameB); got != "B" {
		t.Errorf("chapter b got image %q", got)
	}
}

func TestResolveImageRootRelative(t *testing.T) {
	cfg, root := newImageCfg(t)
	writeFile(t, filepath.Join(root, "images", "logo.png"), "LOGO")
	writeFile(t, filepath.Join(root, "src", "deep", "ch.md"), "")
	name := resolveAndStageImage("/images/logo.png", cfg, filepath.Join(root, "src", "deep", "ch.md"))
	if got := stagedContent(t, cfg, name); got != "LOGO" {
		t.Errorf("got %q", got)
	}
}

func TestResolveImageFallbackPrefersPathSuffix(t *testing.T) {
	cfg, root := newImageCfg(t)
	writeFile(t, filepath.Join(root, "assets", "one", "fig.png"), "ONE")
	writeFile(t, filepath.Join(root, "assets", "two", "fig.png"), "TWO")
	writeFile(t, filepath.Join(root, "chapters", "ch.md"), "")
	// Broken relative link; the filename search should pick the "two/fig.png" candidate.
	name := resolveAndStageImage("../wrong/two/fig.png", cfg, filepath.Join(root, "chapters", "ch.md"))
	if got := stagedContent(t, cfg, name); got != "TWO" {
		t.Errorf("got %q", got)
	}
}

func TestResolveImageRejectsPathsOutsideRoot(t *testing.T) {
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.png"), "SECRET")
	cfg, root := newImageCfg(t)
	writeFile(t, filepath.Join(root, "ch.md"), "")

	rel, err := filepath.Rel(root, filepath.Join(outside, "secret.png"))
	if err != nil {
		t.Fatal(err)
	}
	if name := resolveAndStageImage(rel, cfg, filepath.Join(root, "ch.md")); name != "" {
		t.Errorf("staged %q from outside the book root", name)
	}
	if name := resolveAndStageImage(filepath.Join(outside, "secret.png"), cfg, filepath.Join(root, "ch.md")); name != "" {
		t.Errorf("staged absolute path %q from outside the book root", name)
	}

	if err := os.Symlink(filepath.Join(outside, "secret.png"), filepath.Join(root, "link.png")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	if name := resolveAndStageImage("link.png", cfg, filepath.Join(root, "ch.md")); name != "" {
		t.Errorf("followed symlink out of the book root, staged %q", name)
	}
}

func TestResolveImageWithoutRootUsesPrestaged(t *testing.T) {
	// EPUB builds: no source tree, images were extracted into WorkDir.
	cfg := &config.BookConfig{WorkDir: t.TempDir()}
	writeFile(t, filepath.Join(cfg.WorkDir, "figure.png"), "FIG")
	if name := resolveAndStageImage("../images/figure.png", cfg, "OEBPS/text/ch1.xhtml"); name != "figure.png" {
		t.Errorf("got %q, want figure.png", name)
	}
}

func TestResolveImageSanitizedNameCollision(t *testing.T) {
	cfg, root := newImageCfg(t)
	writeFile(t, filepath.Join(root, "a b", "x.png"), "SPACE")
	writeFile(t, filepath.Join(root, "a_b", "x.png"), "UNDERSCORE")
	n1 := resolveAndStageImage("a b/x.png", cfg, "")
	n2 := resolveAndStageImage("a_b/x.png", cfg, "")
	if n1 == n2 {
		t.Fatalf("distinct sources collapsed onto one staged name %q", n1)
	}
	if stagedContent(t, cfg, n1) != "SPACE" || stagedContent(t, cfg, n2) != "UNDERSCORE" {
		t.Error("staged contents mixed up")
	}
}

func TestResolveImageUnsupportedExtension(t *testing.T) {
	cfg, root := newImageCfg(t)
	writeFile(t, filepath.Join(root, "x.svg"), "<svg/>")
	if name := resolveAndStageImage("x.svg", cfg, ""); name != "" {
		t.Errorf("svg should not be staged, got %q", name)
	}
}
