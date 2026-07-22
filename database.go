package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// ==========================================
// 全域設定與資料結構
// ==========================================
var (
	ReposDir     = "./my_git_repos"
	LfsDir       = "./my_git_repos/.lfs_objects"
	LfsThreshold = 5 * 1024 * 1024
	gitLock      sync.Mutex
)

// (與原本相同的資料結構定義)
type CommitData struct {
	Hexsha  string   `json:"hexsha"`
	Message string   `json:"message"`
	Time    string   `json:"time"`
	Parents []string `json:"parents"`
	Files   []string `json:"files"`
}

type FileData struct {
	Path        string `json:"path"`
	DisplayName string `json:"displayName"`
	Message     string `json:"message"`
	Time        string `json:"time"`
	Hexsha      string `json:"hexsha"`
}

type BranchData struct {
	Current string              `json:"current"`
	All     []string            `json:"all"`
	Tips    map[string][]string `json:"tips"`
}

type RepoDetails struct {
	Commits      []CommitData `json:"commits"`
	GraphCommits []CommitData `json:"graph_commits"`
	Files        []FileData   `json:"files"`
	Branches     BranchData   `json:"branches"`
}

type UploadFile struct {
	Path  string
	Bytes []byte
}

// 🌟 新增：用於裝載 Release 資訊的結構體
type ReleaseAsset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Path string `json:"path"` // 供下載用的相對路徑
}

type ReleaseData struct {
	TagName     string         `json:"tag_name"`
	Message     string         `json:"message"`
	Time        string         `json:"time"`
	CommitSha   string         `json:"commit_sha"`
	Assets      []ReleaseAsset `json:"assets"`
	IsRelease   bool           `json:"is_release"` // 🌟 新增：用來區分它是 Release 還是純 Tag
}


// ==========================================
// LFS 核心引擎 (保持不變)
// ==========================================
func createLFSPointer(fileBytes []byte) []byte {
	if len(fileBytes) < LfsThreshold {
		return fileBytes
	}
	hash := sha256.Sum256(fileBytes)
	hashStr := hex.EncodeToString(hash[:])
	size := len(fileBytes)
	pointer := fmt.Sprintf("version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize %d\n", hashStr, size)
	os.MkdirAll(LfsDir, 0755)
	lfsFilePath := filepath.Join(LfsDir, hashStr)
	if _, err := os.Stat(lfsFilePath); os.IsNotExist(err) {
		os.WriteFile(lfsFilePath, fileBytes, 0644)
	}
	return []byte(pointer)
}

func resolveLFSPointer(fileBytes []byte) []byte {
	prefix := []byte("version https://git-lfs.github.com/spec/v1")
	if !strings.HasPrefix(string(fileBytes), string(prefix)) {
		return fileBytes
	}
	lines := strings.Split(string(fileBytes), "\n")
	var sha256Hash string
	for _, line := range lines {
		if strings.HasPrefix(line, "oid sha256:") {
			sha256Hash = strings.TrimSpace(strings.TrimPrefix(line, "oid sha256:"))
			break
		}
	}
	if sha256Hash != "" {
		lfsFilePath := filepath.Join(LfsDir, sha256Hash)
		if data, err := os.ReadFile(lfsFilePath); err == nil {
			return data
		}
	}
	return fileBytes
}

// ==========================================
// 🌟 核心防護機制：確保 .gitignore 封印了可執行檔
// ==========================================
func EnsureGitignore(repoPath string) {
	ignorePath := filepath.Join(repoPath, ".gitignore")
	
	// 我們要強制封印的黑名單 (不讓執行檔進版本庫)
	blackList := []string{
		"gitlocal-server.exe",
		"gitlocal-server",
		"gitlocal.exe",
		"gitlocal",
		"*.exe",
		"__pycache__/",
		".DS_Store",
		".gitlocal/releases", // 🌟 也要保護發布金庫自己不被 Git 追蹤
	}

	var content string
	if _, err := os.Stat(ignorePath); err == nil {
		data, _ := os.ReadFile(ignorePath)
		content = string(data)
	}

	needsAppend := false
	for _, item := range blackList {
		if !strings.Contains(content, item) {
			content += "\n" + item
			needsAppend = true
		}
	}

	if needsAppend {
		// 追加寫入，不覆蓋原本使用者自訂的規則
		os.WriteFile(ignorePath, []byte(strings.TrimSpace(content)), 0644)
	}
}


// ==========================================
// 倉庫讀寫基礎功能
// ==========================================
func GetAllRepositories() []string {
	var repos []string
	os.MkdirAll(ReposDir, 0755)
	entries, err := os.ReadDir(ReposDir)
	if err != nil { return repos }
	for _, entry := range entries {
		if entry.IsDir() {
			if _, err := os.Stat(filepath.Join(ReposDir, entry.Name(), ".git")); err == nil {
				repos = append(repos, entry.Name())
			}
		}
	}
	return repos
}

// GetRepoDetails 維持原樣... (為節省長度，此處省略，請保留您原本的 GetRepoDetails 實作)
func GetRepoDetails(repoName string) *RepoDetails {
	path := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(path)
	if err != nil { return nil }

	details := &RepoDetails{
		Commits:      []CommitData{},
		GraphCommits: []CommitData{},
		Files:        []FileData{},
		Branches: BranchData{All: []string{}, Tips: make(map[string][]string)},
	}

	headRef, err := repo.Head()
	if err == nil {
		details.Branches.Current = headRef.Name().Short()
	} else {
		details.Branches.Current = "master"
	}

	branchIter, _ := repo.Branches()
	branchIter.ForEach(func(ref *plumbing.Reference) error {
		bName := ref.Name().Short()
		sha := ref.Hash().String()
		details.Branches.All = append(details.Branches.All, bName)
		details.Branches.Tips[sha] = append(details.Branches.Tips[sha], bName)
		return nil
	})

	// 🌟 1. 全分支強制掃描 GraphCommits (確保樹狀圖資料完整)
	visitedCommits := make(map[string]bool)
	details.GraphCommits = []CommitData{}

	var startingHashes []plumbing.Hash
	if headRef != nil {
		startingHashes = append(startingHashes, headRef.Hash())
	}
	if branchIter, err := repo.Branches(); err == nil {
		branchIter.ForEach(func(ref *plumbing.Reference) error {
			startingHashes = append(startingHashes, ref.Hash())
			return nil
		})
	}

	for _, startHash := range startingHashes {
		gIter, err := repo.Log(&git.LogOptions{From: startHash})
		if err != nil { continue }
		gIter.ForEach(func(c *object.Commit) error {
			sha := c.Hash.String()
			if visitedCommits[sha] { return nil }
			visitedCommits[sha] = true

			parents := []string{}
			for _, p := range c.ParentHashes {
				parents = append(parents, p.String())
			}
			details.GraphCommits = append(details.GraphCommits, CommitData{
				Hexsha:  sha,
				Message: strings.TrimSpace(c.Message),
				Time:    c.Author.When.Format("2006-01-02 15:04:05"),
				Parents: parents,
			})
			return nil
		})
	}

	// 🌟 2. 極速且精準的檔案歷史對應 (使用 Tree.Diff + 提早結束機制，絕不卡頓)
	fileLastCommit := make(map[string]*object.Commit)

	if headRef != nil {
		headCommit, err := repo.CommitObject(headRef.Hash())
		if err == nil {
			tree, errTree := headCommit.Tree()
			if errTree == nil {
				// 建立當前專案所有檔案的「待查清單」
				remainingFiles := make(map[string]bool)
				_ = tree.Files().ForEach(func(f *object.File) error {
					remainingFiles[f.Name] = true
					return nil
				})

				// 往回追蹤 Log
				logIter, errLog := repo.Log(&git.LogOptions{From: headRef.Hash()})
				if errLog == nil {
					_ = logIter.ForEach(func(c *object.Commit) error {
						// 💡 關鍵極速魔法：所有檔案都找到最後修改者了，立刻終止，絕不浪費時間！
						if len(remainingFiles) == 0 {
							return io.EOF
						}

						cTree, errCTree := c.Tree()
						if errCTree != nil { return nil }

						if c.NumParents() == 0 {
							// 創世 Commit：裡面的所有檔案都算這顆 Commit 產生的
							_ = cTree.Files().ForEach(func(f *object.File) error {
								if remainingFiles[f.Name] {
									fileLastCommit[f.Name] = c
									delete(remainingFiles, f.Name)
								}
								return nil
							})
						} else {
							// 非創世 Commit：只做極速的 Tree 節點 Diff (不計算內文 Patch)
							parentCommit, errP := c.Parent(0)
							if errP == nil {
								pTree, errPTree := parentCommit.Tree()
								if errPTree == nil {
									changes, errDiff := pTree.Diff(cTree)
									if errDiff == nil {
										for _, ch := range changes {
											fPath := ch.To.Name
											if fPath == "" { fPath = ch.From.Name }
											if remainingFiles[fPath] {
												fileLastCommit[fPath] = c
												delete(remainingFiles, fPath) // 找到了就剔除
											}
										}
									}
								}
							}
						}
						return nil
					})
				}

				// 寫入檔案列表
				_ = tree.Files().ForEach(func(f *object.File) error {
					commitToUse := headCommit
					if lastC, ok := fileLastCommit[f.Name]; ok {
						commitToUse = lastC
					}
					details.Files = append(details.Files, FileData{
						Path:        f.Name,
						DisplayName: filepath.Base(f.Name),
						Message:     strings.TrimSpace(commitToUse.Message),
						Time:        formatRelativeTime(commitToUse.Author.When),
						Hexsha:      commitToUse.Hash.String(),
					})
					return nil
				})
			}
		}
	}

	return details
}

func CreateNewRepo(repoName string) error {
	gitLock.Lock()
	defer gitLock.Unlock()

	path := filepath.Join(ReposDir, repoName)
	os.MkdirAll(path, 0755)
	
	// 建立倉庫時順便打上防護符咒
	EnsureGitignore(path)

	_, err := git.PlainInit(path, false)
	return err
}

func BatchUploadAndCommit(repoName string, files []UploadFile, message string) error {
	gitLock.Lock()
	defer gitLock.Unlock()

	path := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(path)
	if err != nil { return err }

	wt, err := repo.Worktree()
	if err != nil { return err }

	EnsureGitignore(path) // 💡 確保有保護傘

	for _, f := range files {
		cleanPath := strings.TrimLeft(strings.ReplaceAll(f.Path, "\\", "/"), "/")
		fullPath := filepath.Join(path, cleanPath)
		os.MkdirAll(filepath.Dir(fullPath), 0755)

		processedBytes := createLFSPointer(f.Bytes)
		os.WriteFile(fullPath, processedBytes, 0644)
		_, err = wt.Add(cleanPath)
		if err != nil { return err }
	}

	_, err = wt.Commit(message, &git.CommitOptions{
		Author: &object.Signature{Name: "Web User", Email: "web@local.git", When: time.Now()},
		AllowEmptyCommits: true,
	})
	return err
}

// ==========================================
// 分支與檔案控制 API (保持不變)
// ==========================================
func CreateBranch(repoName, branchName, commitSha string) error {
	gitLock.Lock()
	defer gitLock.Unlock()
	repo, err := git.PlainOpen(filepath.Join(ReposDir, repoName))
	if err != nil { return err }
	var hash plumbing.Hash
	if commitSha != "" {
		hash = plumbing.NewHash(commitSha)
	} else {
		headRef, err := repo.Head()
		if err != nil { return err }
		hash = headRef.Hash()
	}
	refName := plumbing.NewBranchReferenceName(branchName)
	ref := plumbing.NewHashReference(refName, hash)
	return repo.Storer.SetReference(ref)
}

func SwitchBranch(repoName, branchName string) error {
	gitLock.Lock()
	defer gitLock.Unlock()
	repo, err := git.PlainOpen(filepath.Join(ReposDir, repoName))
	if err != nil { return err }
	wt, err := repo.Worktree()
	if err != nil { return err }
	return wt.Checkout(&git.CheckoutOptions{Branch: plumbing.NewBranchReferenceName(branchName), Force: true})
}

func DeleteBranch(repoName, branchName string) error {
	gitLock.Lock()
	defer gitLock.Unlock()
	repo, err := git.PlainOpen(filepath.Join(ReposDir, repoName))
	if err != nil { return err }
	headRef, err := repo.Head()
	if err == nil && headRef.Name().Short() == branchName {
		return fmt.Errorf("不能刪除當前正在使用的分支！請先切換到其他分支。")
	}
	refName := plumbing.NewBranchReferenceName(branchName)
	return repo.Storer.RemoveReference(refName)
}

func GetFileContent(repoName, filePath string) (string, error) {
	path := filepath.Join(ReposDir, repoName)
	fullPath := filepath.Join(path, filePath)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return "", nil
	}
	rawBytes, err := os.ReadFile(fullPath)
	if err != nil { return "", err }
	resolvedBytes := resolveLFSPointer(rawBytes)
	return string(resolvedBytes), nil
}

func WriteAndCommit(repoName, filePath, content, message string) error {
	gitLock.Lock()
	defer gitLock.Unlock()
	path := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(path)
	if err != nil { return err }
	wt, err := repo.Worktree()
	if err != nil { return err }

	EnsureGitignore(path) // 💡

	cleanPath := strings.TrimLeft(strings.ReplaceAll(filePath, "\\", "/"), "/")
	fullPath := filepath.Join(path, cleanPath)
	os.MkdirAll(filepath.Dir(fullPath), 0755)
	processedBytes := createLFSPointer([]byte(content))
	err = os.WriteFile(fullPath, processedBytes, 0644)
	if err != nil { return err }
	_, err = wt.Add(cleanPath)
	if err != nil { return err }
	_, err = wt.Commit(message, &git.CommitOptions{Author: &object.Signature{Name: "Web User", Email: "web@local.git", When: time.Now()}, AllowEmptyCommits: true})
	return err
}

func GetCommitDiff(repoName, commitSha string) (map[string]interface{}, error) {
	path := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(path)
	if err != nil { return nil, err }
	commitObj, err := repo.CommitObject(plumbing.NewHash(commitSha))
	if err != nil { return nil, err }
	currentTree, err := commitObj.Tree()
	if err != nil { return nil, err }
	var parentTree *object.Tree
	if commitObj.NumParents() > 0 {
		parentCommit, err := commitObj.Parent(0)
		if err == nil {
			parentTree, _ = parentCommit.Tree()
		}
	}
	changes, err := currentTree.Diff(parentTree)
	if err != nil { return nil, err }
	patch, err := changes.Patch()
	diffsText := ""
	if err == nil {
		diffsText = patch.String()
	} else {
		diffsText = "這是首次提交 (Initial Commit)，沒有變更差異。"
	}
	result := map[string]interface{}{
		"hexsha":  commitObj.Hash.String(),
		"message": strings.TrimSpace(commitObj.Message),
		"author":  commitObj.Author.Name,
		"time":    commitObj.Author.When.Format("2006-01-02 15:04:05"),
		"diffs":   diffsText,
	}
	return result, nil
}

func formatRelativeTime(t time.Time) string {
	exactTime := t.Format("2006-01-02 15:04:05")
	diff := time.Since(t)
	var timeAgo string
	if diff.Hours() >= 24 {
		days := int(diff.Hours() / 24)
		timeAgo = fmt.Sprintf("%d 天前", days)
	} else if diff.Hours() >= 1 {
		hours := int(diff.Hours())
		timeAgo = fmt.Sprintf("%d 小時前", hours)
	} else if diff.Minutes() >= 1 {
		minutes := int(diff.Minutes())
		timeAgo = fmt.Sprintf("%d 分鐘前", minutes)
	} else {
		seconds := int(diff.Seconds())
		if seconds < 0 { seconds = 0 }
		timeAgo = fmt.Sprintf("%d 秒前", seconds)
	}
	return fmt.Sprintf("%s (%s)", timeAgo, exactTime)
}

func GetFileAtCommit(repoName, commitSha, filePath string) ([]byte, string, error) {
	path := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(path)
	if err != nil { return nil, "", err }
	commitObj, err := repo.CommitObject(plumbing.NewHash(commitSha))
	if err != nil { return nil, "", err }
	tree, err := commitObj.Tree()
	if err != nil { return nil, "", err }
	file, err := tree.File(filePath)
	if err != nil { return nil, "", err }
	contents, err := file.Contents()
	if err != nil { return nil, "", err }
	fileBytes := []byte(contents)
	resolvedBytes := resolveLFSPointer(fileBytes)
	timeStr := commitObj.Author.When.Format("20060102_1504")
	return resolvedBytes, timeStr, nil
}

func GetCommitZip(repoName, commitSha string) ([]byte, error) {
	path := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(path)
	if err != nil { return nil, err }
	commitObj, err := repo.CommitObject(plumbing.NewHash(commitSha))
	if err != nil { return nil, err }
	tree, err := commitObj.Tree()
	if err != nil { return nil, err }
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)
	err = tree.Files().ForEach(func(f *object.File) error {
		contents, err := f.Contents()
		if err != nil { return err }
		fileBytes := []byte(contents)
		resolvedBytes := resolveLFSPointer(fileBytes)
		w, err := zipWriter.Create(f.Name)
		if err != nil { return err }
		_, err = w.Write(resolvedBytes)
		return err
	})
	if err != nil { return nil, err }
	zipWriter.Close()
	return buf.Bytes(), nil
}

func MoveAndCommit(repoName, oldPath, newPath string) error {
	gitLock.Lock()
	defer gitLock.Unlock()
	repoDir := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(repoDir)
	if err != nil { return err }
	wt, err := repo.Worktree()
	if err != nil { return err }
	oldFull := filepath.Join(repoDir, oldPath)
	newFull := filepath.Join(repoDir, newPath)
	if _, err := os.Stat(oldFull); os.IsNotExist(err) { return fmt.Errorf("找不到原始檔案: %s", oldPath) }
	os.MkdirAll(filepath.Dir(newFull), 0755)
	if err := os.Rename(oldFull, newFull); err != nil { return err }
	if _, err := wt.Add(newPath); err != nil { return err }
	if _, err := wt.Remove(oldPath); err != nil { return err }
	msg := fmt.Sprintf("🚚 重新命名/移動: %s -> %s", oldPath, newPath)
	_, err = wt.Commit(msg, &git.CommitOptions{Author: &object.Signature{Name: "Web User", Email: "web@local.git", When: time.Now()}, AllowEmptyCommits: true})
	return err
}

func PushToRemote(repoName, remoteURL, token string) error {
	gitLock.Lock()
	defer gitLock.Unlock()
	path := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(path)
	if err != nil { return err }
	headRef, err := repo.Head()
	if err != nil { return err }
	var auth *githttp.BasicAuth
	if token != "" {
		auth = &githttp.BasicAuth{Username: "gitlocal-token", Password: token}
	}
	err = repo.Push(&git.PushOptions{RemoteURL: remoteURL, Auth: auth, RefSpecs: []config.RefSpec{config.RefSpec(headRef.Name() + ":" + headRef.Name())}})
	if err == git.NoErrAlreadyUpToDate { return nil }
	return err
}

func DeleteFileAndCommit(repoName, filePath string) error {
	gitLock.Lock()
	defer gitLock.Unlock()
	repoDir := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(repoDir)
	if err != nil { return err }
	wt, err := repo.Worktree()
	if err != nil { return err }
	cleanPath := strings.TrimLeft(strings.ReplaceAll(filePath, "\\", "/"), "/")
	fullPath := filepath.Join(repoDir, cleanPath)
	if _, err := os.Stat(fullPath); !os.IsNotExist(err) {
		if err := os.Remove(fullPath); err != nil { return err }
	}
	if _, err := wt.Remove(cleanPath); err != nil { return err }
	msg := fmt.Sprintf("🗑️ 刪除檔案: %s", cleanPath)
	_, err = wt.Commit(msg, &git.CommitOptions{Author: &object.Signature{Name: "Web User", Email: "web@local.git", When: time.Now()}, AllowEmptyCommits: true})
	return err
}

type SmartIgnore struct {
	Patterns []string
}

func NewSmartIgnore(repoPath string) *SmartIgnore {
	si := &SmartIgnore{Patterns: []string{".git", ".gitlocal", "gitlocal.py", "gitlocal.bat", "gitlocal.exe", "gitlocal-server", "gitlocal-server.exe", "__pycache__", "my_git_repos", "venv", "env"}}
	ignorePath := filepath.Join(repoPath, ".gitignore")
	if data, err := os.ReadFile(ignorePath); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") { si.Patterns = append(si.Patterns, strings.TrimPrefix(line, "/")) }
		}
	}
	return si
}

func (si *SmartIgnore) IsIgnored(relPath string) bool {
	relPath = strings.ReplaceAll(relPath, "\\", "/")
	parts := strings.Split(relPath, "/")
	for _, pattern := range si.Patterns {
		if matched, _ := filepath.Match(pattern, relPath); matched { return true }
		for _, part := range parts {
			if part == pattern { return true }
			if matched, _ := filepath.Match(pattern, part); matched { return true }
		}
	}
	return false
}

func GetRepoManifest(repoName string) (map[string]string, error) {
	manifest := make(map[string]string)
	repoPath := filepath.Join(ReposDir, repoName)
	if _, err := os.Stat(repoPath); os.IsNotExist(err) { return manifest, nil }
	ignorer := NewSmartIgnore(repoPath)
	err := filepath.WalkDir(repoPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil { return err }
		relPath, _ := filepath.Rel(repoPath, path)
		relPath = strings.ReplaceAll(relPath, "\\", "/")
		if relPath == "." { return nil }
		if ignorer.IsIgnored(relPath) {
			if d.IsDir() { return filepath.SkipDir }
			return nil
		}
		if !d.IsDir() {
			data, err := os.ReadFile(path)
			if err == nil {
				hash := sha1.Sum(data)
				manifest[relPath] = hex.EncodeToString(hash[:])
			}
		}
		return nil
	})
	return manifest, err
}

// ==========================================
// 🌟 Releases 發布與金庫管理 API
// ==========================================

// 建立 Release (打標籤 + 打包源碼 + 封存執行檔)
func CreateRelease(repoName, tagName, message string, assets []UploadFile) error {
	gitLock.Lock()
	defer gitLock.Unlock()

	repoDir := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(repoDir)
	if err != nil { return err }

	headRef, err := repo.Head()
	if err != nil { return err }

	// 1. 建立 Git Tag (標籤)
	_, err = repo.CreateTag(tagName, headRef.Hash(), &git.CreateTagOptions{
		Tagger: &object.Signature{
			Name:  "Web User",
			Email: "web@local.git",
			When:  time.Now(),
		},
		Message: message,
	})
	if err != nil { return err }

	// 2. 建立本地發布專屬金庫資料夾
	releaseDir := filepath.Join(repoDir, ".gitlocal", "releases", tagName)
	os.MkdirAll(releaseDir, 0755)

	// 3. 自動打包當下 Commit 的原始碼成 ZIP
	zipBytes, err := GetCommitZip(repoName, headRef.Hash().String())
	if err == nil {
		sourceZipPath := filepath.Join(releaseDir, fmt.Sprintf("%s_%s_source.zip", repoName, tagName))
		os.WriteFile(sourceZipPath, zipBytes, 0644)
	}

	// 🌟 4. 處理手動上傳的發布檔案 (取代原本的自動搜刮)
	for _, asset := range assets {
		if len(asset.Bytes) > 0 {
			// 將使用者上傳的檔案寫入專屬金庫
			assetPath := filepath.Join(releaseDir, filepath.Base(asset.Path))
			os.WriteFile(assetPath, asset.Bytes, 0644)
		}
	}

	return nil
}

// 內部輔助函數：將編譯好的檔案移動到金庫內
func copyBinaryArtifacts(srcDir, destDir string) {
	// 尋找目標檔名 (包含 .exe 和沒有副檔名的 Linux 二進位檔)
	targets := []string{"*.exe", "gitlocal-server", "gitlocal"}
	for _, pattern := range targets {
		matches, _ := filepath.Glob(filepath.Join(srcDir, pattern))
		for _, match := range matches {
			if info, err := os.Stat(match); err == nil && !info.IsDir() {
				fileName := filepath.Base(match)
				destPath := filepath.Join(destDir, fileName)
				
				// 執行複製
				if sourceFile, err := os.Open(match); err == nil {
					if destFile, err := os.Create(destPath); err == nil {
						io.Copy(destFile, sourceFile)
						destFile.Close()
					}
					sourceFile.Close()
				}
			}
		}
	}
}

// 獲取歷史發布清單
func GetReleasesList(repoName string) ([]ReleaseData, error) {
	repoDir := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(repoDir)
	if err != nil { return nil, err }

	var releases []ReleaseData

	// 走訪所有 Tags
	tags, err := repo.Tags()
	if err != nil { return nil, err }
	
	tags.ForEach(func(ref *plumbing.Reference) error {
		tagName := strings.TrimPrefix(ref.Name().String(), "refs/tags/")
		
		// 嘗試讀取 Tag 物件以獲取註解說明
		var msg, timeStr, shaStr string
		if tagObj, err := repo.TagObject(ref.Hash()); err == nil {
			msg = tagObj.Message
			timeStr = tagObj.Tagger.When.Format("2006-01-02 15:04:05")
			shaStr = tagObj.Target.String()
		} else {
			// 若為 Lightweight Tag (輕量級標籤，無附屬訊息)
			if commitObj, err := repo.CommitObject(ref.Hash()); err == nil {
				msg = "無發布說明 (輕量級標籤)"
				timeStr = commitObj.Author.When.Format("2006-01-02 15:04:05")
				shaStr = commitObj.Hash.String()
			}
		}

		// 🌟 掃描金庫內的 Assets 檔案清單，並判斷是否為真實發布
		var assets []ReleaseAsset
		releaseDir := filepath.Join(repoDir, ".gitlocal", "releases", tagName)
		isRelease := false // 預設為純標籤
		
		// 只有當專屬資料夾存在時，才承認它是一個 Release
		if info, err := os.Stat(releaseDir); err == nil && info.IsDir() {
			isRelease = true
			if entries, err := os.ReadDir(releaseDir); err == nil {
				for _, entry := range entries {
					if !entry.IsDir() {
						if fileInfo, err := entry.Info(); err == nil {
							assets = append(assets, ReleaseAsset{
								Name: entry.Name(),
								Size: fileInfo.Size(),
								Path: fmt.Sprintf("%s/%s", tagName, entry.Name()), 
							})
						}
					}
				}
			}
		}

		releases = append(releases, ReleaseData{
			TagName:   tagName,
			Message:   strings.TrimSpace(msg),
			Time:      timeStr,
			CommitSha: shaStr,
			Assets:    assets,
			IsRelease: isRelease, // 🌟 傳遞狀態給前端
		})
		return nil
	})
	
	// 反轉陣列，讓最新的 Tag 顯示在最上面
	for i, j := 0, len(releases)-1; i < j; i, j = i+1, j-1 {
		releases[i], releases[j] = releases[j], releases[i]
	}

	return releases, nil
}

// 🌟 新增：刪除 Release (解開 Git Tag + 徹底抹除金庫檔案)
func DeleteRelease(repoName, tagName string) error {
	gitLock.Lock()
	defer gitLock.Unlock()

	repoDir := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(repoDir)
	if err != nil { return err }

	// 1. 精準移除 Git 中的 Tag 參照
	refName := plumbing.NewTagReferenceName(tagName)
	err = repo.Storer.RemoveReference(refName)
	// 如果標籤本來就不存在，當作沒事發生；其他錯誤才回傳
	if err != nil && err != plumbing.ErrReferenceNotFound { 
		return err 
	}

	// 2. 徹底摧毀本地發布金庫資料夾 (.gitlocal/releases/<tag_name>)
	releaseDir := filepath.Join(repoDir, ".gitlocal", "releases", tagName)
	if err := os.RemoveAll(releaseDir); err != nil {
		return err
	}

	return nil
}

// 🌟 最終極版：編輯 Release (真實搬移金庫，將舊版降級為純標籤)
func EditRelease(repoName, oldTagName, newTagName, message string, assets []UploadFile) error {
	gitLock.Lock()
	defer gitLock.Unlock()

	repoDir := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(repoDir)
	if err != nil { return err }

	ref, err := repo.Tag(oldTagName)
	if err != nil { return fmt.Errorf("找不到舊標籤 %s: %v", oldTagName, err) }

	var targetSha plumbing.Hash
	tagObj, err := repo.TagObject(ref.Hash())
	if err == nil { targetSha = tagObj.Target } else { targetSha = ref.Hash() }

	// 1. 保留舊標籤！只在同名更新時才移除舊標籤
	if oldTagName != newTagName {
		repo.Storer.RemoveReference(plumbing.NewTagReferenceName(newTagName))
	} else {
		repo.Storer.RemoveReference(plumbing.NewTagReferenceName(oldTagName))
	}

	// 2. 建立新標籤
	safeMsg := strings.TrimSpace(message)
	if safeMsg == "" { safeMsg = "Release " + newTagName }
	_, err = repo.CreateTag(newTagName, targetSha, &git.CreateTagOptions{
		Tagger: &object.Signature{Name: "Web User", Email: "web@local.git", When: time.Now()},
		Message: safeMsg,
	})
	if err != nil { return err }

	// 3. 🌟 處理金庫資料夾 (執行搬家，舊標籤失去資料夾後將被降級為純標籤)
	oldReleaseDir := filepath.Join(repoDir, ".gitlocal", "releases", oldTagName)
	newReleaseDir := filepath.Join(repoDir, ".gitlocal", "releases", newTagName)

	if oldTagName != newTagName {
		if _, err := os.Stat(oldReleaseDir); err == nil {
			// 防呆：若目標已有金庫，先移除以確保搬移成功
			os.RemoveAll(newReleaseDir)
			
			// 直接將舊金庫重新命名為新金庫 (搬家)
			os.Rename(oldReleaseDir, newReleaseDir)
			
			// 順便把裡面自動打包的 source.zip 檔名更新
			oldZipName := fmt.Sprintf("%s_%s_source.zip", repoName, oldTagName)
			newZipName := fmt.Sprintf("%s_%s_source.zip", repoName, newTagName)
			os.Rename(filepath.Join(newReleaseDir, oldZipName), filepath.Join(newReleaseDir, newZipName))
		} else {
			os.MkdirAll(newReleaseDir, 0755)
		}
	} else {
		os.MkdirAll(newReleaseDir, 0755)
	}

	// 4. 寫入新檔案
	for _, asset := range assets {
		if len(asset.Bytes) > 0 {
			assetPath := filepath.Join(newReleaseDir, filepath.Base(asset.Path))
			os.WriteFile(assetPath, asset.Bytes, 0644)
		}
	}

	return nil
}

// ==========================================
// 🌟 視覺化合併 (3-Way Merge) 核心引擎
// ==========================================

type MergeConflict struct {
	Path   string `json:"path"`
	Ours   string `json:"ours"`
	Theirs string `json:"theirs"`
}

// 1. 啟動合併：掃描並回傳兩條分支有差異的檔案
func InitiateMerge(repoName, sourceBranch string) ([]MergeConflict, error) {
	gitLock.Lock()
	defer gitLock.Unlock()

	repo, err := git.PlainOpen(filepath.Join(ReposDir, repoName))
	if err != nil { return nil, err }

	headRef, err := repo.Head()
	if err != nil { return nil, err }

	sourceRef, err := repo.Reference(plumbing.NewBranchReferenceName(sourceBranch), true)
	if err != nil { return nil, err }

	oursCommit, _ := repo.CommitObject(headRef.Hash())
	theirsCommit, _ := repo.CommitObject(sourceRef.Hash())
	oursTree, _ := oursCommit.Tree()
	theirsTree, _ := theirsCommit.Tree()

	// 比對兩棵樹的差異
	changes, err := oursTree.Diff(theirsTree)
	if err != nil { return nil, err }

	var conflicts []MergeConflict
	for _, ch := range changes {
		conflict := MergeConflict{}
		
		if ch.From.Name == "" && ch.To.Name != "" { 
			// 來源分支新增的檔案
			conflict.Path = ch.To.Name
			fTheirs, _ := theirsTree.File(ch.To.Name)
			conflict.Theirs, _ = fTheirs.Contents()
		} else if ch.From.Name != "" && ch.To.Name == "" { 
			// 來源分支刪除的檔案
			conflict.Path = ch.From.Name
			fOurs, _ := oursTree.File(ch.From.Name)
			conflict.Ours, _ = fOurs.Contents()
		} else { 
			// 雙方都有修改的檔案
			conflict.Path = ch.To.Name
			fOurs, _ := oursTree.File(ch.From.Name)
			conflict.Ours, _ = fOurs.Contents()
			
			fTheirs, _ := theirsTree.File(ch.To.Name)
			conflict.Theirs, _ = fTheirs.Contents()
		}
		conflicts = append(conflicts, conflict)
	}
	return conflicts, nil
}

// 2. 封裝合併：寫入使用者在網頁上決定的最終結果，並產生雙親節點 (Merge Commit)
func FinalizeMerge(repoName, sourceBranch, message string, resolvedFiles map[string]string) error {
	gitLock.Lock()
	defer gitLock.Unlock()

	path := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(path)
	if err != nil { return err }

	wt, err := repo.Worktree()
	if err != nil { return err }

	headRef, _ := repo.Head()
	sourceRef, _ := repo.Reference(plumbing.NewBranchReferenceName(sourceBranch), true)

	// 覆寫為使用者在前端決定好的最終程式碼
	for fPath, content := range resolvedFiles {
		fullPath := filepath.Join(path, fPath)
		if content == "" {
			os.Remove(fullPath)
			wt.Remove(fPath)
		} else {
			os.MkdirAll(filepath.Dir(fullPath), 0755)
			os.WriteFile(fullPath, []byte(content), 0644)
			wt.Add(fPath)
		}
	}

	// 🌟 關鍵魔法：賦予這個 Commit 兩個父親，讓 Git 樹狀圖畫出 Y 字型合併線！
	_, err = wt.Commit(message, &git.CommitOptions{
		Author:  &object.Signature{Name: "Web User", Email: "web@local.git", When: time.Now()},
		Parents: []plumbing.Hash{headRef.Hash(), sourceRef.Hash()},
	})
	return err
}

// ==========================================
// 🌟 高效能非同步分頁 API 專用結構
// ==========================================

type CommitPageResponse struct {
	TotalCount int          `json:"total_count"`
	TotalPages int          `json:"total_pages"`
	Page       int          `json:"page"`
	Commits    []CommitData `json:"commits"`
}

func GetPaginatedCommits(repoName string, page int, limit int) (*CommitPageResponse, error) {
	gitLock.Lock()
	defer gitLock.Unlock()

	path := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(path)
	if err != nil { return nil, err }

	headRef, err := repo.Head()
	if err != nil { return nil, err }

	logIter, err := repo.Log(&git.LogOptions{From: headRef.Hash()})
	if err != nil { return nil, err }

	offset := (page - 1) * limit
	total := 0
	var commits []CommitData

	// 極速遍歷：只算總數，遇到落入區間的才做耗時的 Stats() 處理
	err = logIter.ForEach(func(c *object.Commit) error {
		if total >= offset && total < offset+limit {
			parents := []string{}
			for _, p := range c.ParentHashes { parents = append(parents, p.String()) }
			
			var changedFiles []string
			// 🌟 只有這 25 筆會執行耗時的 c.Stats()
			stats, errStat := c.Stats()
			if errStat == nil {
				for _, stat := range stats { changedFiles = append(changedFiles, stat.Name) }
			}

			commitData := CommitData{
				Hexsha:  c.Hash.String(),
				Message: strings.TrimSpace(c.Message),
				Time:    c.Author.When.Format("2006-01-02 15:04:05"),
				Parents: parents,
				Files:   changedFiles,
			}
			commits = append(commits, commitData)
		}
		total++
		return nil
	})

	totalPages := total / limit
	if total%limit != 0 { totalPages++ }

	return &CommitPageResponse{
		TotalCount: total,
		TotalPages: totalPages,
		Page:       page,
		Commits:    commits,
	}, nil
}

