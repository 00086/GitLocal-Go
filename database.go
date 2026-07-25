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
	"os/exec" // 👈 貼在這行：新增執行系統命令套件
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
	gitLock      sync.Mutex // 僅用於寫入操作 (Write-Lock)
)

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

type ReleaseAsset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Path string `json:"path"`
}

type ReleaseData struct {
	TagName   string         `json:"tag_name"`
	Message   string         `json:"message"`
	Time      string         `json:"time"`
	CommitSha string         `json:"commit_sha"`
	Assets    []ReleaseAsset `json:"assets"`
	IsRelease bool           `json:"is_release"`
}

type GitGraphResponse struct {
	GraphCommits []CommitData `json:"graph_commits"`
	Branches     BranchData   `json:"branches"`
}

// 🌟 新增：倉庫健康度資料結構
type RepoHealth struct {
	GitDirSize string `json:"git_dir_size"`
}

// 🌟 1. 取得倉庫容量健康診斷
func GetRepoHealth(repoName string) (*RepoHealth, error) {
	repoPath := filepath.Join(ReposDir, repoName)
	gitDir := filepath.Join(repoPath, ".git")

	var size int64
	err := filepath.Walk(gitDir, func(_ string, info os.FileInfo, err error) error {
		if err != nil { return err }
		if !info.IsDir() { size += info.Size() }
		return nil
	})
	if err != nil { return nil, err }

	sizeMB := float64(size) / (1024 * 1024)
	sizeStr := fmt.Sprintf("%.2f MB", sizeMB)
	if sizeMB < 1.0 {
		sizeStr = fmt.Sprintf("%.2f KB", float64(size)/1024)
	}

	return &RepoHealth{GitDirSize: sizeStr}, nil
}

// 🌟 2. 執行一鍵 Git GC 垃圾回收瘦身 (純 Go 原生版，無需安裝系統 Git)
func RunGitGC(repoName string) (string, error) {
	gitLock.Lock()
	defer gitLock.Unlock()
	invalidateRepoCache(repoName)

	repoPath := filepath.Join(ReposDir, repoName)

	// 1. 若系統有安裝 Git，優先調用原生命令；若無安裝，自動接管無縫執行 Go 原生清理
	if _, err := exec.LookPath("git"); err == nil {
		cmd := exec.Command("git", "gc", "--prune=now")
		cmd.Dir = repoPath
		out, err := cmd.CombinedOutput()
		if err == nil {
			return "🎉 使用 Git 原生引擎瘦身成功！\n" + strings.TrimSpace(string(out)), nil
		}
	}

	// 🌟 2.【Go 原生無依賴瘦身引擎】(完全不依賴外部 git.exe)
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", fmt.Errorf("開啟倉庫失敗: %v", err)
	}

	// 步驟 A: 走訪所有分支與標籤，收集所有「可達 (Reachable)」的物件 Hash
	reachableHashes := make(map[plumbing.Hash]bool)
	refIter, err := repo.References()
	if err == nil {
		_ = refIter.ForEach(func(ref *plumbing.Reference) error {
			startingHash := ref.Hash()
			logIter, err := repo.Log(&git.LogOptions{From: startingHash})
			if err == nil {
				_ = logIter.ForEach(func(c *object.Commit) error {
					reachableHashes[c.Hash] = true
					if tree, err := c.Tree(); err == nil {
						reachableHashes[tree.Hash] = true
						_ = tree.Files().ForEach(func(f *object.File) error {
							reachableHashes[f.Blob.Hash] = true
							return nil
						})
					}
					return nil
				})
			}
			return nil
		})
	}

	// 步驟 B: 掃描 .git/objects/ 目錄，清理孤立 (Unreachable) 散落物件與臨時鎖定檔
	objectsDir := filepath.Join(repoPath, ".git", "objects")
	var deletedCount int
	var freedBytes int64

	_ = filepath.WalkDir(objectsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() { return nil }

		// 清理臨時鎖定與打包暫存檔
		if strings.HasPrefix(d.Name(), "tmp_") || strings.HasSuffix(d.Name(), ".lock") {
			if info, err := d.Info(); err == nil {
				freedBytes += info.Size()
				_ = os.Remove(path)
				deletedCount++
			}
			return nil
		}

		// 解析散落物件的 SHA-1 Hash (例如 objects/1a/2b3c4d... -> 1a2b3c4d...)
		rel, _ := filepath.Rel(objectsDir, path)
		rel = strings.ReplaceAll(rel, "\\", "/")
		parts := strings.Split(rel, "/")
		if len(parts) == 2 && len(parts[0]) == 2 {
			hashStr := parts[0] + parts[1]
			hash := plumbing.NewHash(hashStr)

			// 若該散落物件已無法從任何分支連通 (孤立廢棄物)，直接刪除釋放空間！
			if !reachableHashes[hash] {
				if info, err := d.Info(); err == nil {
					freedBytes += info.Size()
					_ = os.Remove(path)
					deletedCount++
				}
			}
		}
		return nil
	})

	// 步驟 C: 順手清理空資料夾
	_ = filepath.WalkDir(objectsDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && path != objectsDir {
			entries, _ := os.ReadDir(path)
			if len(entries) == 0 {
				_ = os.Remove(path)
			}
		}
		return nil
	})

	sizeMB := float64(freedBytes) / (1024 * 1024)
	if sizeMB < 0.01 && freedBytes > 0 {
		return fmt.Sprintf("🎉 Go 原生瘦身完成！清理了 %d 個孤立廢棄物件/鎖定檔 (%.2f KB)。", deletedCount, float64(freedBytes)/1024), nil
	}
	return fmt.Sprintf("🎉 Go 原生瘦身完成！清理了 %d 個孤立廢棄物件/鎖定檔，共釋放 %.2f MB 硬碟空間。", deletedCount, sizeMB), nil
}

// 🌟 3. Web 終端安全白名單指令執行器 (自動綁定當前網頁專案名稱)
func RunWebTerminalCmd(repoName string, commandStr string) (string, error) {
	commandStr = strings.TrimSpace(commandStr)
	if commandStr == "" { return "", fmt.Errorf("指令不能為空") }

	isGit := strings.HasPrefix(commandStr, "git ") || commandStr == "git"
	isGitLocal := strings.HasPrefix(commandStr, "gitlocal ") || commandStr == "gitlocal"

	if !isGit && !isGitLocal {
		return "", fmt.Errorf("基於安全考量，僅支援以 'git' 或 'gitlocal' 開頭的指令！")
	}

	for _, forbidden := range []string{"&", "|", ";", ">", "<", "`", "$"} {
		if strings.Contains(commandStr, forbidden) {
			return "", fmt.Errorf("禁止使用管道或多重指令符號 (%s)", forbidden)
		}
	}

	args := strings.Fields(commandStr)
	repoPath := filepath.Join(ReposDir, repoName)

	execBinary := args[0]
	cmdDir := repoPath
	execArgs := args[1:]

	// 🌟 關鍵修復：執行 gitlocal 時，若使用者省略專案名稱，自動代入網頁目前的 repoName
	if isGitLocal {
		absExe, _ := filepath.Abs("gitlocal.exe")
		if _, err := os.Stat(absExe); err == nil {
			execBinary = absExe
		} else {
			absBin, _ := filepath.Abs("gitlocal")
			if _, err := os.Stat(absBin); err == nil {
				execBinary = absBin
			}
		}
		cmdDir, _ = filepath.Abs(".")

		knownCmds := map[string]bool{
			"status": true, "commit": true, "branch": true,
			"switch": true, "checkout": true, "delete-branch": true,
			"merge": true, "push": true, "get-file": true, "zip": true,
		}

		// 若第二個參數是已知指令（代表省略了專案名），自動插入當前頁面的 repoName
		if len(args) >= 2 && knownCmds[args[1]] {
			execArgs = append([]string{repoName}, args[1:]...)
		}
	}

	cmd := exec.Command(execBinary, execArgs...)
	cmd.Dir = cmdDir

	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return fmt.Sprintf("❌ 執行失敗: %v\n(請確認已在伺服器根目錄執行 'go build gitlocal.go' 編譯出 gitlocal.exe)", err), nil
	}

	if strings.Contains(commandStr, "checkout") || strings.Contains(commandStr, "commit") ||
	   strings.Contains(commandStr, "branch") || strings.Contains(commandStr, "merge") ||
	   strings.Contains(commandStr, "reset") || strings.Contains(commandStr, "rebase") {
		invalidateRepoCache(repoName)
	}

	return string(out), nil
}

// ==========================================
// ⚡ 記憶體快取引擎 (In-Memory Cache)
// ==========================================
type RepoCacheItem struct {
	GitGraph *GitGraphResponse
	FileList []FileData
	Details  *RepoDetails
}

type MemoryCache struct {
	sync.RWMutex
	items map[string]*RepoCacheItem
}

var repoCache = &MemoryCache{
	items: make(map[string]*RepoCacheItem),
}

// 🌟 1. 清除快取 (當專案有任何寫入/變更時自動觸發)
func invalidateRepoCache(repoName string) {
	repoCache.Lock()
	defer repoCache.Unlock()
	delete(repoCache.items, repoName)
}

// 🌟 2. 手動清空全域快取
func ClearAllMemoryCache() {
	repoCache.Lock()
	defer repoCache.Unlock()
	repoCache.items = make(map[string]*RepoCacheItem)
}

// ==========================================
// LFS 核心引擎
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

func EnsureGitignore(repoPath string) {
	ignorePath := filepath.Join(repoPath, ".gitignore")
	blackList := []string{
		"gitlocal-server.exe", "gitlocal-server", "gitlocal.exe", "gitlocal",
		"*.exe", "__pycache__/", ".DS_Store", ".gitlocal/releases",
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
		os.WriteFile(ignorePath, []byte(strings.TrimSpace(content)), 0644)
	}
}

// ==========================================
// 倉庫讀寫與模組化核心
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

func scanGraphCommits(repo *git.Repository) []CommitData {
	visited := make(map[plumbing.Hash]bool)
	graphCommits := []CommitData{}

	startingHashes := []plumbing.Hash{}

	if headRef, err := repo.Head(); err == nil {
		startingHashes = append(startingHashes, headRef.Hash())
	}

	if bIter, err := repo.Branches(); err == nil {
		_ = bIter.ForEach(func(ref *plumbing.Reference) error {
			startingHashes = append(startingHashes, ref.Hash())
			return nil
		})
	}

	if tIter, err := repo.Tags(); err == nil {
		_ = tIter.ForEach(func(ref *plumbing.Reference) error {
			if tagObj, err := repo.TagObject(ref.Hash()); err == nil {
				startingHashes = append(startingHashes, tagObj.Target)
			} else {
				startingHashes = append(startingHashes, ref.Hash())
			}
			return nil
		})
	}

	for _, startHash := range startingHashes {
		logIter, err := repo.Log(&git.LogOptions{From: startHash})
		if err != nil { continue }

		_ = logIter.ForEach(func(c *object.Commit) error {
			if visited[c.Hash] { return nil }
			visited[c.Hash] = true

			parents := make([]string, 0, len(c.ParentHashes))
			for _, p := range c.ParentHashes {
				parents = append(parents, p.String())
			}

			graphCommits = append(graphCommits, CommitData{
				Hexsha:  c.Hash.String(),
				Message: strings.TrimSpace(c.Message),
				Time:    c.Author.When.Format("2006-01-02 15:04:05"),
				Parents: parents,
			})
			return nil
		})
	}

	return graphCommits
}

func buildFileList(repo *git.Repository, headCommit *object.Commit) []FileData {
	tree, err := headCommit.Tree()
	if err != nil { return nil }

	fileMap := make(map[string]*FileData)
	remaining := make(map[string]bool)

	_ = tree.Files().ForEach(func(f *object.File) error {
		fileMap[f.Name] = &FileData{
			Path:        f.Name,
			DisplayName: filepath.Base(f.Name),
			Message:     strings.TrimSpace(headCommit.Message),
			Time:        headCommit.Author.When.Format("2006-01-02 15:04:05"), // 🌟 改為這行
			Hexsha:      headCommit.Hash.String(),
		}
		remaining[f.Name] = true
		return nil
	})

	if len(remaining) == 0 { return []FileData{} }

	logIter, errLog := repo.Log(&git.LogOptions{From: headCommit.Hash})
	if errLog == nil {
		_ = logIter.ForEach(func(c *object.Commit) error {
			if len(remaining) == 0 { return io.EOF }

			if c.NumParents() == 0 {
				for p := range remaining {
					fileMap[p].Message = strings.TrimSpace(c.Message)
					fileMap[p].Time = c.Author.When.Format("2006-01-02 15:04:05") // 🌟 改為這行
					fileMap[p].Hexsha = c.Hash.String()
				}
				remaining = make(map[string]bool)
				return io.EOF
			}

			pCommit, errP := c.Parent(0)
			if errP != nil { return nil }

			cTree, errC := c.Tree()
			pTree, errP2 := pCommit.Tree()
			if errC != nil || errP2 != nil { return nil }

			if cTree.Hash == pTree.Hash { return nil }

			changes, errDiff := object.DiffTree(pTree, cTree)
			if errDiff != nil { return nil }

			for _, ch := range changes {
				targetPath := ch.To.Name
				if targetPath == "" { targetPath = ch.From.Name }

				if remaining[targetPath] {
					fileMap[targetPath].Message = strings.TrimSpace(c.Message)
					fileMap[targetPath].Time = c.Author.When.Format("2006-01-02 15:04:05") // 🌟 改為這行
					fileMap[targetPath].Hexsha = c.Hash.String()
					delete(remaining, targetPath)
				}
			}

			return nil
		})
	}

	result := make([]FileData, 0, len(fileMap))
	_ = tree.Files().ForEach(func(f *object.File) error {
		if fd, ok := fileMap[f.Name]; ok {
			result = append(result, *fd)
		}
		return nil
	})
	return result
}

// 🌟 快取優化：GetRepoDetails
func GetRepoDetails(repoName string) *RepoDetails {
	repoCache.RLock()
	if item, ok := repoCache.items[repoName]; ok && item.Details != nil {
		repoCache.RUnlock()
		return item.Details
	}
	repoCache.RUnlock()

	path := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(path)
	if err != nil { return nil }

	details := &RepoDetails{
		Commits:      []CommitData{},
		GraphCommits: []CommitData{},
		Files:        []FileData{},
		Branches:     BranchData{All: []string{}, Tips: make(map[string][]string)},
	}

	headRef, err := repo.Head()
	if err == nil {
		details.Branches.Current = headRef.Name().Short()
	} else {
		details.Branches.Current = "master"
	}

	branchSet := make(map[string]bool)
	if branchIter, err := repo.Branches(); err == nil {
		_ = branchIter.ForEach(func(ref *plumbing.Reference) error {
			bName := ref.Name().Short()
			sha := ref.Hash().String()
			if !branchSet[bName] {
				branchSet[bName] = true
				details.Branches.All = append(details.Branches.All, bName)
			}
			details.Branches.Tips[sha] = append(details.Branches.Tips[sha], bName)
			return nil
		})
	}

	if headRef != nil {
		if headCommit, err := repo.CommitObject(headRef.Hash()); err == nil {
			details.Files = buildFileList(repo, headCommit)
		}
	}

	// 存入快取
	repoCache.Lock()
	if repoCache.items[repoName] == nil {
		repoCache.items[repoName] = &RepoCacheItem{}
	}
	repoCache.items[repoName].Details = details
	repoCache.Unlock()

	return details
}

// 🌟 快取優化：GetFileCommitsDetails
func GetFileCommitsDetails(repoName string) ([]FileData, error) {
	repoCache.RLock()
	if item, ok := repoCache.items[repoName]; ok && item.FileList != nil {
		repoCache.RUnlock()
		return item.FileList, nil
	}
	repoCache.RUnlock()

	path := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(path)
	if err != nil { return nil, err }

	headRef, err := repo.Head()
	if err != nil { return nil, err }

	headCommit, err := repo.CommitObject(headRef.Hash())
	if err != nil { return nil, err }

	files := buildFileList(repo, headCommit)

	// 存入快取
	repoCache.Lock()
	if repoCache.items[repoName] == nil {
		repoCache.items[repoName] = &RepoCacheItem{}
	}
	repoCache.items[repoName].FileList = files
	repoCache.Unlock()

	return files, nil
}

// 🌟 快取優化：GetGitGraphData (⚡ 樹狀圖瞬間由記憶體回應，0.1ms)
func GetGitGraphData(repoName string) (*GitGraphResponse, error) {
	repoCache.RLock()
	if item, ok := repoCache.items[repoName]; ok && item.GitGraph != nil {
		repoCache.RUnlock()
		return item.GitGraph, nil
	}
	repoCache.RUnlock()

	path := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(path)
	if err != nil { return nil, err }

	headRef, _ := repo.Head()

	branchData := BranchData{All: []string{}, Tips: make(map[string][]string)}
	if headRef != nil { branchData.Current = headRef.Name().Short() } else { branchData.Current = "master" }

	branchSet := make(map[string]bool)
	if branchIter, err := repo.Branches(); err == nil {
		_ = branchIter.ForEach(func(ref *plumbing.Reference) error {
			bName := ref.Name().Short()
			sha := ref.Hash().String()
			if !branchSet[bName] {
				branchSet[bName] = true
				branchData.All = append(branchData.All, bName)
			}
			branchData.Tips[sha] = append(branchData.Tips[sha], bName)
			return nil
		})
	}

	graphCommits := scanGraphCommits(repo)

	resp := &GitGraphResponse{
		GraphCommits: graphCommits,
		Branches:     branchData,
	}

	// 存入快取
	repoCache.Lock()
	if repoCache.items[repoName] == nil {
		repoCache.items[repoName] = &RepoCacheItem{}
	}
	repoCache.items[repoName].GitGraph = resp
	repoCache.Unlock()

	return resp, nil
}

func CreateNewRepo(repoName string) error {
	gitLock.Lock()
	defer gitLock.Unlock()
	invalidateRepoCache(repoName) // 🌟 寫入後清空快取

	path := filepath.Join(ReposDir, repoName)
	os.MkdirAll(path, 0755)
	EnsureGitignore(path)
	_, err := git.PlainInit(path, false)
	return err
}

func BatchUploadAndCommit(repoName string, files []UploadFile, message string) error {
	gitLock.Lock()
	defer gitLock.Unlock()
	invalidateRepoCache(repoName) // 🌟 寫入後清空快取

	path := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(path)
	if err != nil { return err }
	wt, err := repo.Worktree()
	if err != nil { return err }

	EnsureGitignore(path)

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
// 分支與檔案控制 API
// ==========================================
func CreateBranch(repoName, branchName, commitSha string) error {
	gitLock.Lock()
	defer gitLock.Unlock()
	invalidateRepoCache(repoName) // 🌟 寫入後清空快取

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
	invalidateRepoCache(repoName) // 🌟 寫入後清空快取

	repo, err := git.PlainOpen(filepath.Join(ReposDir, repoName))
	if err != nil { return err }
	wt, err := repo.Worktree()
	if err != nil { return err }
	return wt.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branchName),
		Force:  true,
	})
}

func DeleteBranch(repoName, branchName string) error {
	gitLock.Lock()
	defer gitLock.Unlock()
	invalidateRepoCache(repoName) // 🌟 寫入後清空快取

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
	invalidateRepoCache(repoName) // 🌟 寫入後清空快取

	path := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(path)
	if err != nil { return err }
	wt, err := repo.Worktree()
	if err != nil { return err }

	EnsureGitignore(path)

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
		diffsText = "這是首次提交 (Initial Commit)，沒有變更檔案差異。"
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
	invalidateRepoCache(repoName) // 🌟 寫入後清空快取

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
	invalidateRepoCache(repoName) // 🌟 寫入後清空快取

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
// Releases 發布管理
// ==========================================
func CreateRelease(repoName, tagName, message string, assets []UploadFile) error {
	gitLock.Lock()
	defer gitLock.Unlock()
	invalidateRepoCache(repoName) // 🌟 寫入後清空快取

	repoDir := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(repoDir)
	if err != nil { return err }

	headRef, err := repo.Head()
	if err != nil { return err }

	_, err = repo.CreateTag(tagName, headRef.Hash(), &git.CreateTagOptions{
		Tagger:  &object.Signature{Name: "Web User", Email: "web@local.git", When: time.Now()},
		Message: message,
	})
	if err != nil { return err }

	releaseDir := filepath.Join(repoDir, ".gitlocal", "releases", tagName)
	os.MkdirAll(releaseDir, 0755)

	zipBytes, err := GetCommitZip(repoName, headRef.Hash().String())
	if err == nil {
		sourceZipPath := filepath.Join(releaseDir, fmt.Sprintf("%s_%s_source.zip", repoName, tagName))
		os.WriteFile(sourceZipPath, zipBytes, 0644)
	}

	for _, asset := range assets {
		if len(asset.Bytes) > 0 {
			assetPath := filepath.Join(releaseDir, filepath.Base(asset.Path))
			os.WriteFile(assetPath, asset.Bytes, 0644)
		}
	}
	return nil
}

func GetReleasesList(repoName string) ([]ReleaseData, error) {
	repoDir := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(repoDir)
	if err != nil { return nil, err }

	var releases []ReleaseData
	tags, err := repo.Tags()
	if err != nil { return nil, err }
	
	_ = tags.ForEach(func(ref *plumbing.Reference) error {
		tagName := strings.TrimPrefix(ref.Name().String(), "refs/tags/")
		var msg, timeStr, shaStr string
		if tagObj, err := repo.TagObject(ref.Hash()); err == nil {
			msg = tagObj.Message
			timeStr = tagObj.Tagger.When.Format("2006-01-02 15:04:05")
			shaStr = tagObj.Target.String()
		} else {
			if commitObj, err := repo.CommitObject(ref.Hash()); err == nil {
				msg = "無發布說明 (輕量級標籤)"
				timeStr = commitObj.Author.When.Format("2006-01-02 15:04:05")
				shaStr = commitObj.Hash.String()
			}
		}

		var assets []ReleaseAsset
		releaseDir := filepath.Join(repoDir, ".gitlocal", "releases", tagName)
		isRelease := false
		
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
			IsRelease: isRelease,
		})
		return nil
	})
	
	for i, j := 0, len(releases)-1; i < j; i, j = i+1, j-1 {
		releases[i], releases[j] = releases[j], releases[i]
	}

	return releases, nil
}

func DeleteRelease(repoName, tagName string) error {
	gitLock.Lock()
	defer gitLock.Unlock()
	invalidateRepoCache(repoName) // 🌟 寫入後清空快取

	repoDir := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(repoDir)
	if err != nil { return err }

	refName := plumbing.NewTagReferenceName(tagName)
	err = repo.Storer.RemoveReference(refName)
	if err != nil && err != plumbing.ErrReferenceNotFound { return err }

	releaseDir := filepath.Join(repoDir, ".gitlocal", "releases", tagName)
	return os.RemoveAll(releaseDir)
}

func EditRelease(repoName, oldTagName, newTagName, message string, assets []UploadFile) error {
	gitLock.Lock()
	defer gitLock.Unlock()
	invalidateRepoCache(repoName) // 🌟 寫入後清空快取

	repoDir := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(repoDir)
	if err != nil { return err }

	ref, err := repo.Tag(oldTagName)
	if err != nil { return fmt.Errorf("找不到舊標籤 %s: %v", oldTagName, err) }

	var targetSha plumbing.Hash
	tagObj, err := repo.TagObject(ref.Hash())
	if err == nil { targetSha = tagObj.Target } else { targetSha = ref.Hash() }

	if oldTagName != newTagName {
		repo.Storer.RemoveReference(plumbing.NewTagReferenceName(newTagName))
	} else {
		repo.Storer.RemoveReference(plumbing.NewTagReferenceName(oldTagName))
	}

	safeMsg := strings.TrimSpace(message)
	if safeMsg == "" { safeMsg = "Release " + newTagName }
	_, err = repo.CreateTag(newTagName, targetSha, &git.CreateTagOptions{
		Tagger:  &object.Signature{Name: "Web User", Email: "web@local.git", When: time.Now()},
		Message: safeMsg,
	})
	if err != nil { return err }

	oldReleaseDir := filepath.Join(repoDir, ".gitlocal", "releases", oldTagName)
	newReleaseDir := filepath.Join(repoDir, ".gitlocal", "releases", newTagName)

	if oldTagName != newTagName {
		if _, err := os.Stat(oldReleaseDir); err == nil {
			os.RemoveAll(newReleaseDir)
			os.Rename(oldReleaseDir, newReleaseDir)
			oldZipName := fmt.Sprintf("%s_%s_source.zip", repoName, oldTagName)
			newZipName := fmt.Sprintf("%s_%s_source.zip", repoName, newTagName)
			os.Rename(filepath.Join(newReleaseDir, oldZipName), filepath.Join(newReleaseDir, newZipName))
		} else {
			os.MkdirAll(newReleaseDir, 0755)
		}
	} else {
		os.MkdirAll(newReleaseDir, 0755)
	}

	for _, asset := range assets {
		if len(asset.Bytes) > 0 {
			assetPath := filepath.Join(newReleaseDir, filepath.Base(asset.Path))
			os.WriteFile(assetPath, asset.Bytes, 0644)
		}
	}
	return nil
}

// ==========================================
// 3-Way Merge 引擎
// ==========================================
type MergeConflict struct {
	Path   string `json:"path"`
	Ours   string `json:"ours"`
	Theirs string `json:"theirs"`
}

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

	changes, err := oursTree.Diff(theirsTree)
	if err != nil { return nil, err }

	var conflicts []MergeConflict
	for _, ch := range changes {
		conflict := MergeConflict{}
		if ch.From.Name == "" && ch.To.Name != "" { 
			conflict.Path = ch.To.Name
			fTheirs, _ := theirsTree.File(ch.To.Name)
			conflict.Theirs, _ = fTheirs.Contents()
		} else if ch.From.Name != "" && ch.To.Name == "" { 
			conflict.Path = ch.From.Name
			fOurs, _ := oursTree.File(ch.From.Name)
			conflict.Ours, _ = fOurs.Contents()
		} else { 
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

func FinalizeMerge(repoName, sourceBranch, message string, resolvedFiles map[string]string) error {
	gitLock.Lock()
	defer gitLock.Unlock()
	invalidateRepoCache(repoName) // 🌟 合併成功後清空快取

	path := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(path)
	if err != nil { return err }

	wt, err := repo.Worktree()
	if err != nil { return err }

	headRef, _ := repo.Head()
	sourceRef, _ := repo.Reference(plumbing.NewBranchReferenceName(sourceBranch), true)

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

	_, err = wt.Commit(message, &git.CommitOptions{
		Author:  &object.Signature{Name: "Web User", Email: "web@local.git", When: time.Now()},
		Parents: []plumbing.Hash{headRef.Hash(), sourceRef.Hash()},
	})
	return err
}

// ==========================================
// 非同步 Commit 分頁 API (1ms 極速回應)
// ==========================================
type CommitPageResponse struct {
	TotalCount int          `json:"total_count"`
	TotalPages int          `json:"total_pages"`
	Page       int          `json:"page"`
	Commits    []CommitData `json:"commits"`
}

func GetPaginatedCommits(repoName string, page int, limit int) (*CommitPageResponse, error) {
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

	err = logIter.ForEach(func(c *object.Commit) error {
		if total >= offset && total < offset+limit {
			parents := []string{}
			for _, p := range c.ParentHashes { parents = append(parents, p.String()) }

			var changedFiles []string
			cTree, errC := c.Tree()
			if errC == nil {
				if c.NumParents() > 0 {
					if parentCommit, errP := c.Parent(0); errP == nil {
						if pTree, errPT := parentCommit.Tree(); errPT == nil {
							if cTree.Hash != pTree.Hash {
								if changes, errDiff := object.DiffTree(pTree, cTree); errDiff == nil {
									for _, ch := range changes {
										fPath := ch.To.Name
										if fPath == "" { fPath = ch.From.Name }
										changedFiles = append(changedFiles, fPath)
									}
								}
							}
						}
					}
				} else {
					_ = cTree.Files().ForEach(func(f *object.File) error {
						changedFiles = append(changedFiles, f.Name)
						return nil
					})
				}
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
	if total%limit != 0 && total > 0 { totalPages++ }

	return &CommitPageResponse{
		TotalCount: total,
		TotalPages: totalPages,
		Page:       page,
		Commits:    commits,
	}, nil
}

// ==========================================
// 🌟 一鍵撤銷 Commit 核心引擎 (純 Go 原生實作)
// ==========================================
func RevertCommit(repoName, commitSha string) error {
	gitLock.Lock()
	defer gitLock.Unlock()
	invalidateRepoCache(repoName) // 寫入後自動刷新記憶體快取

	path := filepath.Join(ReposDir, repoName)
	repo, err := git.PlainOpen(path)
	if err != nil { return fmt.Errorf("開啟倉庫失敗: %v", err) }

	wt, err := repo.Worktree()
	if err != nil { return err }

	_, err = repo.Head()
	if err != nil { return fmt.Errorf("無法獲取 HEAD 參考: %v", err) }

	targetCommit, err := repo.CommitObject(plumbing.NewHash(commitSha))
	if err != nil { return fmt.Errorf("找不到目標 Commit: %v", err) }

	if targetCommit.NumParents() == 0 {
		return fmt.Errorf("無法撤銷初始提交 (Initial Commit)！")
	}

	parentCommit, err := targetCommit.Parent(0)
	if err != nil { return fmt.Errorf("無法獲取父提交: %v", err) }

	targetTree, err := targetCommit.Tree()
	if err != nil { return err }

	parentTree, err := parentCommit.Tree()
	if err != nil { return err }

	// 🌟 核心計算：對比 Target 與 Parent 的反向差異 (Target -> Parent)
	changes, err := targetTree.Diff(parentTree)
	if err != nil { return err }

	if len(changes) == 0 {
		return fmt.Errorf("此 Commit 無任何可撤銷的異動內容")
	}

	// 套用反向異動至當前工作區
	for _, ch := range changes {
		if ch.From.Name == "" && ch.To.Name != "" {
			// 原本被刪除 -> 重新還原檔案
			file, err := parentTree.File(ch.To.Name)
			if err == nil {
				content, _ := file.Contents()
				fullPath := filepath.Join(path, ch.To.Name)
				os.MkdirAll(filepath.Dir(fullPath), 0755)
				os.WriteFile(fullPath, []byte(content), 0644)
				wt.Add(ch.To.Name)
			}
		} else if ch.From.Name != "" && ch.To.Name == "" {
			// 原本被新增 -> 刪除該檔案
			cleanPath := strings.TrimLeft(strings.ReplaceAll(ch.From.Name, "\\", "/"), "/")
			fullPath := filepath.Join(path, cleanPath)
			os.Remove(fullPath)
			wt.Remove(cleanPath)
		} else {
			// 原本被修改 -> 恢復成 Parent 版本的內容
			file, err := parentTree.File(ch.To.Name)
			if err == nil {
				content, _ := file.Contents()
				fullPath := filepath.Join(path, ch.To.Name)
				os.MkdirAll(filepath.Dir(fullPath), 0755)
				os.WriteFile(fullPath, []byte(content), 0644)
				wt.Add(ch.To.Name)
			}
		}
	}

	shortSha := commitSha
	if len(shortSha) >= 7 { shortSha = shortSha[:7] }
	firstMsgLine := strings.Split(strings.TrimSpace(targetCommit.Message), "\n")[0]
	revertMsg := fmt.Sprintf("↺ Revert \"%s\"\n\nThis reverts commit %s.", firstMsgLine, commitSha)

	// 提交反向修正 Commit
	_, err = wt.Commit(revertMsg, &git.CommitOptions{
		Author:            &object.Signature{Name: "Web User", Email: "web@local.git", When: time.Now()},
		AllowEmptyCommits: false,
	})
	return err
}

// 🌟 打包包含 .git 的完整倉庫目錄 (連同所有分支與歷史)
func GetFullRepoZip(repoName string) ([]byte, error) {
	path := filepath.Join(ReposDir, repoName)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("找不到倉庫目錄")
	}

	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	err := filepath.WalkDir(path, func(filePath string, d fs.DirEntry, err error) error {
		if err != nil { return err }

		relPath, err := filepath.Rel(path, filePath)
		if err != nil || relPath == "." { return nil }

		relPath = strings.ReplaceAll(relPath, "\\", "/")

		if d.IsDir() {
			_, err = zipWriter.Create(relPath + "/")
			return err
		}

		data, err := os.ReadFile(filePath)
		if err != nil { return err }

		w, err := zipWriter.Create(relPath)
		if err != nil { return err }
		_, err = w.Write(data)
		return err
	})

	if err != nil { return nil, err }
	zipWriter.Close()
	return buf.Bytes(), nil
}

// ==========================================
// 🌟 智慧匯入專案 (自動識別 Full Backup 或純源碼 ZIP)
// ==========================================
func ImportRepoFromZip(repoName string, zipBytes []byte) error {
	gitLock.Lock()
	defer gitLock.Unlock()
	invalidateRepoCache(repoName)

	targetDir := filepath.Join(ReposDir, repoName)
	if _, err := os.Stat(targetDir); err == nil {
		return fmt.Errorf("專案 [%s] 已存在，請使用其他名稱或先刪除舊專案", repoName)
	}

	zipReader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return fmt.Errorf("無效的 ZIP 檔案: %v", err)
	}

	os.MkdirAll(targetDir, 0755)

	hasGitDir := false
	for _, f := range zipReader.File {
		cleanPath := strings.TrimLeft(strings.ReplaceAll(f.Name, "\\", "/"), "/")
		
		// 檢查是否包含 .git 目錄結構
		if strings.Contains(cleanPath, ".git/") || cleanPath == ".git" {
			hasGitDir = true
		}

		fullPath := filepath.Join(targetDir, cleanPath)

		if f.FileInfo().IsDir() {
			os.MkdirAll(fullPath, 0755)
			continue
		}

		os.MkdirAll(filepath.Dir(fullPath), 0755)
		rc, err := f.Open()
		if err != nil { continue }

		outFile, err := os.Create(fullPath)
		if err != nil {
			rc.Close()
			continue
		}
		io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
	}

	// 🌟 情境 2：若純源碼 ZIP (無 .git)，自動化初始化並建立初始 Commit
	if !hasGitDir {
		EnsureGitignore(targetDir)
		repo, err := git.PlainInit(targetDir, false)
		if err != nil { return fmt.Errorf("初始化新倉庫失敗: %v", err) }

		wt, err := repo.Worktree()
		if err == nil {
			_ = wt.AddGlob(".")
			_, _ = wt.Commit("🎉 匯入專案並建立初始提交 (Initial import from ZIP)", &git.CommitOptions{
				Author:            &object.Signature{Name: "Web User", Email: "web@local.git", When: time.Now()},
				AllowEmptyCommits: true,
			})
		}
	}

	return nil
}

