package assetservice

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/francisconeves97/jxscout/internal/core/errutil"
)

const (
	urlDelimiter = "/"
)

type SaveFileRequest struct {
	// PathURL is the path structure of the file to be saved, corresponds to a URL that maps into the filesystem.
	// This path should be / separated and internally we will handle the OS filesystem. This is for ease of use
	// because in reality this will be a URL
	PathURL string
	// Content is the file content
	Content    *string
	JavaScript bool
}

type FileService interface {
	SaveInSubfolder(ctx context.Context, subfolder string, req SaveFileRequest) (string, error)
	UpdateWorkingDirectory(newPath string)
	FileExists(pathURL string, subfolder string) (bool, error)
}

type fileServiceImpl struct {
	workingDirectory string
	log              *slog.Logger
}

func NewFileService(workingDirectory string, log *slog.Logger) FileService {
	return &fileServiceImpl{
		workingDirectory: workingDirectory,
		log:              log,
	}
}

func (s *fileServiceImpl) SaveInSubfolder(ctx context.Context, subfolder string, req SaveFileRequest) (string, error) {
	filePath := []string{
		s.workingDirectory,
		subfolder,
	}

	return s.save(ctx, filePath, req)
}

func cleanWindows(p string) string {
	// This regex should ideally only be applied to the filename component, not the whole path.
	// Applying to the whole path might remove valid characters from directory names if they are unusual.
	// However, keeping original logic for now.
	m1 := regexp.MustCompile(`[?%*|:"<>()]`)
	return m1.ReplaceAllString(p, "")
}

func (s *fileServiceImpl) save(ctx context.Context, filePath []string, req SaveFileRequest) (string, error) {
	targetPath, err := s.urlToPath(req.PathURL, filePath)
	if err != nil {
		return "", errutil.Wrap(err, "failed to convert url to path")
	}

	if req.JavaScript && !strings.HasSuffix(strings.ToLower(targetPath), ".js") {
		targetPath += ".js"
	}
	targetPath, err = s.SimpleSave(targetPath, req.Content)
	if err != nil {
		return "", errutil.Wrap(err, "failed to write file")
	}

	return targetPath, nil
}

func (s *fileServiceImpl) urlToPath(pathURL string, filePath []string) (string, error) {
	parsedURL, err := url.Parse(pathURL)
	if err != nil {
		return "", errutil.Wrap(err, "failed to parse url")
	}

	filePath = append(filePath, parsedURL.Host)

	path := parsedURL.Path
	pathParts := strings.Split(path, urlDelimiter)

	filePath = append(filePath, pathParts...)

	targetPath := filepath.Join(filePath...)
	if parsedURL.RawQuery != "" {
		ext := filepath.Ext(targetPath)
		hash := sha256.Sum256([]byte(parsedURL.RawQuery))
		targetPath = strings.TrimSuffix(targetPath, ext) + fmt.Sprintf(".%x", hash[:8]) + ext
	}

	if runtime.GOOS == "windows" {
		base := filepath.Base(targetPath)
		dir := filepath.Dir(targetPath)
		cleanedBase := cleanWindows(base)
		targetPath = filepath.Join(dir, cleanedBase)
	}

	targetPath = filepath.Clean(targetPath)

	return targetPath, nil
}

func (s *fileServiceImpl) URLToPath(pathURL string, subfolder string) (string, error) {
	return s.urlToPath(pathURL, []string{
		s.workingDirectory,
		subfolder,
	})
}

func (s *fileServiceImpl) FileExists(pathURL string, subfolder string) (bool, error) {
	filePath, err := s.URLToPath(pathURL, subfolder)
	if err != nil {
		return false, errutil.Wrap(err, "failed to convert url to path")
	}

	_, err = os.Stat(filePath)
	if os.IsNotExist(err) {
		_, err = os.Stat(filePath + ".js")
	}
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}

	return false, errutil.Wrap(err, "failed to check if file exists")
}

func (s *fileServiceImpl) SimpleSave(filePath string, content *string) (string, error) {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return filePath, errutil.Wrap(err, "failed to create directory")
	}

	s.log.Debug("saving to file", "filepath", filePath, "dir", dir)

	err := os.WriteFile(filePath, []byte(*content), 0644)
	if err != nil {
		return filePath, errutil.Wrap(err, "failed to create file and write content")
	}

	return filePath, nil
}

func (s *fileServiceImpl) UpdateWorkingDirectory(path string) {
	s.workingDirectory = path
}
