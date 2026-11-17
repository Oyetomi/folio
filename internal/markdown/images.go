package markdown

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Oyetomi/folio/internal/config"
)

// maxRemoteImageBytes caps downloads of remote images referenced by a
// manuscript, so a hostile or broken URL cannot exhaust memory.
const maxRemoteImageBytes = 20 << 20

var stageableImageExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".pdf": true}

// resolveAndStageImage locates the image a chapter references, copies it into
// cfg.WorkDir under a name unique to its source path, and returns that name
// for \includegraphics. It returns "" when the image cannot be found or is not
// a format XeLaTeX can include.
//
// Local paths are resolved, in order:
//  1. relative to the referencing chapter file (standard Markdown semantics);
//  2. relative to the book root (mdBook/GitBook-style "/images/x.png");
//  3. images already staged in WorkDir by the EPUB or notebook readers;
//  4. by filename anywhere in the book root, preferring the candidate whose
//     path ends with the referenced path (tolerates sloppy relative links).
//
// Resolved files must lie inside cfg.RootDir, so a manuscript cannot pull
// arbitrary local files into the PDF via "../../" paths.
func resolveAndStageImage(imgPath string, cfg *config.BookConfig, docPath string) string {
	if cfg == nil || cfg.WorkDir == "" || imgPath == "" {
		return ""
	}
	workDir := cfg.WorkDir
	if strings.Contains(imgPath, "goreportcard.com") || strings.Contains(imgPath, "shields.io") || strings.Contains(imgPath, "travis-ci") {
		return ""
	}
	if strings.HasPrefix(imgPath, "http://") || strings.HasPrefix(imgPath, "https://") {
		return stageRemoteImage(imgPath, workDir)
	}

	// Drop URL fragments/queries and decode the common %20 escape.
	if i := strings.IndexAny(imgPath, "?#"); i >= 0 {
		imgPath = imgPath[:i]
	}
	imgPath = strings.ReplaceAll(imgPath, "%20", " ")

	base := filepath.Base(imgPath)
	if !stageableImageExt[strings.ToLower(filepath.Ext(base))] {
		return ""
	}

	rootDir := cfg.RootDir
	if rootDir != "" {
		// 1. Relative to the chapter file (only when it really is on disk;
		//    EPUB chapters carry archive-internal paths).
		if docPath != "" && !filepath.IsAbs(imgPath) {
			if st, err := os.Stat(docPath); err == nil && !st.IsDir() {
				if name := stageLocalImage(filepath.Join(filepath.Dir(docPath), imgPath), rootDir, workDir); name != "" {
					return name
				}
			}
		}
		// 2. Relative to the book root.
		if name := stageLocalImage(filepath.Join(rootDir, strings.TrimPrefix(filepath.ToSlash(imgPath), "/")), rootDir, workDir); name != "" {
			return name
		}
	}

	// 3. Already staged by the EPUB / ipynb readers.
	san := SanitizeImageFilename(base)
	for _, cand := range []string{san, base} {
		if st, err := os.Stat(filepath.Join(workDir, cand)); err == nil && !st.IsDir() && st.Size() > 0 {
			return cand
		}
	}

	// 4. Search the book tree by filename.
	if rootDir != "" {
		if found := findImageByName(rootDir, imgPath); found != "" {
			return stageLocalImage(found, rootDir, workDir)
		}
	}
	return ""
}

// stageLocalImage copies src into workDir and returns the staged name, or ""
// if src does not exist or lies outside rootDir.
func stageLocalImage(src, rootDir, workDir string) string {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return ""
	}
	absSrc, err := filepath.Abs(src)
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(absSrc); err == nil {
		absSrc = real
	} else {
		return ""
	}
	if real, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = real
	}
	rel, err := filepath.Rel(absRoot, absSrc)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	st, err := os.Stat(absSrc)
	if err != nil || st.IsDir() || st.Size() == 0 {
		return ""
	}
	data, err := os.ReadFile(absSrc)
	if err != nil {
		return ""
	}

	// Name the staged copy after its path inside the book so that
	// ch01/img/diagram.png and ch02/img/diagram.png stay distinct.
	name := SanitizeImageFilename(strings.ReplaceAll(filepath.ToSlash(rel), "/", "_"))
	dst := filepath.Join(workDir, name)
	if existing, err := os.ReadFile(dst); err == nil {
		if bytes.Equal(existing, data) {
			return name
		}
		// Sanitizing collapsed two different paths onto one name.
		sum := sha1.Sum([]byte(rel))
		ext := filepath.Ext(name)
		name = strings.TrimSuffix(name, ext) + "-" + hex.EncodeToString(sum[:4]) + ext
		dst = filepath.Join(workDir, name)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return ""
	}
	return name
}

var imageIndexCache sync.Map // abs rootDir -> map[lower basename][]abs path

// findImageByName returns the image under rootDir whose filename matches
// imgPath's, preferring the one whose path shares the longest suffix with
// imgPath. Candidates are visited in lexical order, so the result is stable.
func findImageByName(rootDir, imgPath string) string {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return ""
	}
	idxAny, ok := imageIndexCache.Load(absRoot)
	if !ok {
		idx := map[string][]string{}
		_ = filepath.WalkDir(absRoot, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				switch d.Name() {
				case ".git", ".github", ".vscode", ".idea", "node_modules", "vendor":
					return filepath.SkipDir
				}
				return nil
			}
			if stageableImageExt[strings.ToLower(filepath.Ext(d.Name()))] {
				key := strings.ToLower(d.Name())
				idx[key] = append(idx[key], p)
			}
			return nil
		})
		idxAny, _ = imageIndexCache.LoadOrStore(absRoot, idx)
	}
	cands := idxAny.(map[string][]string)[strings.ToLower(filepath.Base(imgPath))]
	if len(cands) == 0 {
		return ""
	}
	want := strings.Split(strings.ToLower(filepath.ToSlash(filepath.Clean(imgPath))), "/")
	best, bestScore := cands[0], -1
	for _, c := range cands {
		have := strings.Split(strings.ToLower(filepath.ToSlash(c)), "/")
		score := 0
		for score < len(want) && score < len(have) && want[len(want)-1-score] == have[len(have)-1-score] {
			score++
		}
		if score > bestScore {
			best, bestScore = c, score
		}
	}
	return best
}

func stageRemoteImage(url, workDir string) string {
	ext := strings.ToLower(filepath.Ext(url))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
		return ""
	}
	san := SanitizeImageFilename(filepath.Base(url))
	dst := filepath.Join(workDir, san)
	if st, err := os.Stat(dst); err == nil && st.Size() > 100 {
		return san
	}
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRemoteImageBytes+1))
	if err != nil || len(data) < 100 || len(data) > maxRemoteImageBytes {
		return ""
	}
	ct := http.DetectContentType(data)
	if !strings.HasPrefix(ct, "image/png") && !strings.HasPrefix(ct, "image/jpeg") {
		return ""
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return ""
	}
	return san
}
