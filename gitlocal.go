//go:build ignore

package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/url" // 🌟 新增：用於發送表單 (POST) 請求
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// 全域設定
var ServerURL string

// ==========================================
// 1. 初始化與設定檔讀取
// ==========================================
func initServerURL() {
	ServerURL = "http://127.0.0.1:5001" // 預設值
	data, err := os.ReadFile(".gitlocal")
	if err == nil {
		lines := strings.Split(string(data), "\n")
		if len(lines) > 0 && strings.TrimSpace(lines[0]) != "" {
			ServerURL = strings.TrimSpace(lines[0])
		}
	}
	ServerURL = strings.TrimRight(ServerURL, "/")
}

// ==========================================
// 2. SmartIgnore (.gitignore 解析引擎)
// ==========================================
type SmartIgnore struct {
	patterns []string
}

func NewSmartIgnore(rootDir string) *SmartIgnore {
	si := &SmartIgnore{
		patterns: []string{".git", ".gitlocal", "gitlocal.py", "gitlocal.exe", "gitlocal", "__pycache__", "my_git_repos", "venv", "env"},
	}
	ignorePath := filepath.Join(rootDir, ".gitignore")
	data, err := os.ReadFile(ignorePath)
	if err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				si.patterns = append(si.patterns, strings.Trim(line, "/"))
			}
		}
	}
	return si
}

func (si *SmartIgnore) IsIgnored(relPath string) bool {
	relPath = strings.ReplaceAll(relPath, "\\", "/")
	parts := strings.Split(relPath, "/")
	
	for _, pattern := range si.patterns {
		for _, part := range parts {
			if part == pattern { return true }
			matched, _ := filepath.Match(pattern, part)
			if matched { return true }
		}
		matched, _ := filepath.Match(pattern, relPath)
		if matched { return true }
	}
	return false
}

// ==========================================
// 3. 核心 API 通訊與平行運算
// ==========================================
func getServerManifest(repoName string) (map[string]string, error) {
	urlStr := fmt.Sprintf("%s/api/repo/%s/manifest", ServerURL, repoName)
	resp, err := http.Get(urlStr)
	if err != nil { return nil, err }
	defer resp.Body.Close()

	var manifest map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

type FileHash struct {
	RelPath string
	Hash    string
	Err     error
}

func calcLocalHashes(workDir string, filesToHash []string) map[string]string {
	localManifest := make(map[string]string)
	results := make(chan FileHash, len(filesToHash))
	var wg sync.WaitGroup

	semaphore := make(chan struct{}, 100)

	for _, relPath := range filesToHash {
		wg.Add(1)
		go func(rp string) {
			defer wg.Done()
			semaphore <- struct{}{}        
			defer func() { <-semaphore }() 

			fullPath := filepath.Join(workDir, rp)
			f, err := os.Open(fullPath)
			if err != nil {
				results <- FileHash{RelPath: rp, Err: err}
				return
			}
			defer f.Close()

			h := sha1.New()
			if _, err := io.Copy(h, f); err != nil {
				results <- FileHash{RelPath: rp, Err: err}
				return
			}
			results <- FileHash{RelPath: rp, Hash: hex.EncodeToString(h.Sum(nil))}
		}(relPath)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for res := range results {
		if res.Err == nil { localManifest[res.RelPath] = res.Hash }
	}
	return localManifest
}

// ==========================================
// 4. 指令實作 (Status & Commit)
// ==========================================
func commandStatus(repoName, workDir string) {
	fmt.Printf("🔍 正在與伺服器 [%s] 進行平行狀態比對...\n", ServerURL)
	serverManifest, err := getServerManifest(repoName)
	if err != nil {
		fmt.Printf("❌ 無法連線至伺服器: %v\n", err)
		return
	}

	ignorer := NewSmartIgnore(workDir)
	var filesToHash []string
	localTracked := make(map[string]bool)

	filepath.WalkDir(workDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil { return nil }
		relPath, _ := filepath.Rel(workDir, path)
		relPath = strings.ReplaceAll(relPath, "\\", "/")

		if relPath == "." { return nil }
		if ignorer.IsIgnored(relPath) {
			if d.IsDir() { return filepath.SkipDir }
			return nil
		}

		if !d.IsDir() {
			localTracked[relPath] = true
			filesToHash = append(filesToHash, relPath)
		}
		return nil
	})

	localManifest := calcLocalHashes(workDir, filesToHash)

	var newFiles, modFiles, delFiles []string
	for rp, localHash := range localManifest {
		serverHash, exists := serverManifest[rp]
		if !exists {
			newFiles = append(newFiles, rp)
		} else if localHash != serverHash {
			modFiles = append(modFiles, rp)
		}
	}
	// 🌟 修正：檢查遠端有、但本地沒掃描到的檔案時，必須確保它「不在忽略名單內」，才判定為已刪除
	for rp := range serverManifest {
		if !localTracked[rp] && !ignorer.IsIgnored(rp) { 
			delFiles = append(delFiles, rp) 
		}
	}

	if len(newFiles) == 0 && len(modFiles) == 0 && len(delFiles) == 0 {
		fmt.Println("✅ 您的專案目前與伺服器完美同步，沒有任何未提交的變更。")
		return
	}
	
	if len(newFiles) > 0 {
		fmt.Println("\n➕ 未追蹤的新檔案:")
		for _, f := range newFiles { fmt.Printf("  [新檔案] %s\n", f) }
	}
	if len(modFiles) > 0 {
		fmt.Println("\n✏️  已修改的檔案:")
		for _, f := range modFiles { fmt.Printf("  [已修改] %s\n", f) }
	}
	if len(delFiles) > 0 {
		fmt.Println("\n🗑️  已刪除的檔案 (存在於伺服器但本地遺失):")
		for _, f := range delFiles { fmt.Printf("  [已刪除] %s\n", f) }
	}
}

func commandCommit(repoName, message, workDir string) {
	fmt.Printf("🔍 正在掃描並比對 [%s] 的差異...\n", repoName)
	serverManifest, err := getServerManifest(repoName)
	if err != nil {
		fmt.Printf("❌ 無法連線至伺服器: %v\n", err)
		return
	}

	ignorer := NewSmartIgnore(workDir)
	var filesToHash []string
	
	filepath.WalkDir(workDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil { return nil }
		relPath, _ := filepath.Rel(workDir, path)
		relPath = strings.ReplaceAll(relPath, "\\", "/")
		
		if relPath == "." { return nil }
		if ignorer.IsIgnored(relPath) {
			if d.IsDir() { return filepath.SkipDir }
			return nil
		}
		if !d.IsDir() { filesToHash = append(filesToHash, relPath) }
		return nil
	})

	localManifest := calcLocalHashes(workDir, filesToHash)

	var filesToUpload []string
	for rp, localHash := range localManifest {
		serverHash, exists := serverManifest[rp]
		if !exists || localHash != serverHash {
			filesToUpload = append(filesToUpload, rp)
		}
	}

	if len(filesToUpload) == 0 {
		fmt.Println("✅ 沒有偵測到任何變更，無需 Commit。")
		return
	}

	fmt.Printf("📦 偵測到 %d 個檔案變更：\n", len(filesToUpload))
	for _, f := range filesToUpload { fmt.Printf("   - %s\n", f) }

	multiLineMsg := fmt.Sprintf("%s\n\n🚀 透過 GitLocal CLI (Go極速版) 自動提交\n📦 異動檔案清單 (%d 個)：\n", message, len(filesToUpload))
	for _, f := range filesToUpload { multiLineMsg += fmt.Sprintf("  - %s\n", f) }

	urlStr := fmt.Sprintf("%s/repo/%s/batch_upload", ServerURL, repoName)
	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	w.WriteField("message", strings.TrimSpace(multiLineMsg))

	for _, relPath := range filesToUpload {
		fullPath := filepath.Join(workDir, relPath)
		file, err := os.Open(fullPath)
		if err != nil { continue }

		// 🌟 關鍵補丁：將完整相對路徑作為獨立欄位傳給伺服器，避免資料夾結構被 Go 伺服器閹割
		w.WriteField("paths", relPath)

		fw, err := w.CreateFormFile("files", relPath)
		if err == nil { io.Copy(fw, file) }
		file.Close()
	}
	w.Close()

	fmt.Println("🚀 正在批次打包並提交到伺服器...")
	req, err := http.NewRequest("POST", urlStr, &b)
	if err != nil { return }
	req.Header.Set("Content-Type", w.FormDataContentType())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("❌ 上傳失敗: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		fmt.Println("🎉 提交成功！")
	} else {
		bodyBytes, _ := io.ReadAll(resp.Body)
		fmt.Printf("❌ 伺服器錯誤 (%d): %s\n", resp.StatusCode, string(bodyBytes))
	}
}

// ==========================================
// 🌟 5. 新增：分支與下載相關指令 API 對接
// ==========================================

// 通用的 POST 請求輔助函數
func sendPostForm(actionName, urlStr string, formData url.Values) {
	fmt.Printf("⏳ 正在通知伺服器執行 %s...\n", actionName)
	resp, err := http.PostForm(urlStr, formData)
	if err != nil {
		fmt.Printf("❌ 連線失敗: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		fmt.Printf("🎉 %s 成功！\n", actionName)
	} else {
		bodyBytes, _ := io.ReadAll(resp.Body)
		fmt.Printf("❌ 伺服器回傳錯誤 (%d): %s\n", resp.StatusCode, string(bodyBytes))
	}
}

func commandBranch(repoName, branchName, commitSha string) {
	urlStr := fmt.Sprintf("%s/repo/%s/branch/create", ServerURL, repoName)
	data := url.Values{"branch_name": {branchName}}
	if commitSha != "" {
		data["commit_sha"] = []string{commitSha}
	}
	sendPostForm("建立分支", urlStr, data)
}

func commandCheckout(repoName, branchName string) {
	urlStr := fmt.Sprintf("%s/repo/%s/branch/switch", ServerURL, repoName)
	sendPostForm("切換分支", urlStr, url.Values{"branch_name": {branchName}})
}

func commandDeleteBranch(repoName, branchName string) {
	urlStr := fmt.Sprintf("%s/repo/%s/branch/delete", ServerURL, repoName)
	sendPostForm("刪除分支", urlStr, url.Values{"branch_name": {branchName}})
}

func commandMerge(repoName, sourceBranch string) {
	urlStr := fmt.Sprintf("%s/repo/%s/branch/merge", ServerURL, repoName)
	sendPostForm("合併分支", urlStr, url.Values{"source_branch": {sourceBranch}})
}

func commandPush(repoName, remoteURL, token string) {
	urlStr := fmt.Sprintf("%s/repo/%s/push", ServerURL, repoName)
	sendPostForm("雲端同步 (Push)", urlStr, url.Values{
		"remote_url": {remoteURL},
		"token":      {token},
	})
}

// 通用的 GET 下載輔助函數
func downloadFile(urlStr, savePath string) {
	fmt.Printf("⬇️  正在從伺服器下載檔案...\n")
	resp, err := http.Get(urlStr)
	if err != nil {
		fmt.Printf("❌ 下載連線失敗: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		fmt.Printf("❌ 伺服器回傳錯誤 (%d): %s\n", resp.StatusCode, string(bodyBytes))
		return
	}

	// 確保目標資料夾存在
	os.MkdirAll(filepath.Dir(savePath), os.ModePerm)
	outFile, err := os.Create(savePath)
	if err != nil {
		fmt.Printf("❌ 無法建立本地檔案: %v\n", err)
		return
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		fmt.Printf("❌ 寫入檔案失敗: %v\n", err)
	} else {
		fmt.Printf("✅ 下載成功！檔案已儲存至: %s\n", savePath)
	}
}

func commandGetFile(repoName, commitSha, filePath, savePath string) {
	// 將路徑中的反斜線統一為斜線以利網址組合
	cleanPath := strings.ReplaceAll(filePath, "\\", "/")
	urlStr := fmt.Sprintf("%s/repo/%s/download/%s/%s", ServerURL, repoName, commitSha, cleanPath)
	downloadFile(urlStr, savePath)
}

func commandZip(repoName, commitSha, savePath string) {
	urlStr := fmt.Sprintf("%s/repo/%s/download_zip/%s", ServerURL, repoName, commitSha)
	downloadFile(urlStr, savePath)
}

// ==========================================
// 6. CLI 路由總管與說明選單
// ==========================================
func printUsage() {
	fmt.Println(`================================================================================
 🚀 GitLocal 遠端指令列工具 (Full API Client - Go 極速版)
================================================================================
 基本語法:
   gitlocal <專案名稱> <指令> [參數]

 🛠️ [基礎操作] 
  (1) status        : 🌟 純粹查看本地與遠端伺服器的檔案差異狀態 (不上傳)
      語法: gitlocal <專案> status

  (2) commit        : 智慧比對並上傳變更的檔案到伺服器
      語法: gitlocal <專案> commit -m "<說明>" <檔案或.>

 🌿 [分支管理]
  (3) branch        : 建立新分支 (可選定特定歷史起點)
      語法: gitlocal <專案> branch <新分支名> [commit_sha]
      
  (4) checkout      : 讓伺服器切換工作目錄至指定分支
      語法: gitlocal <專案> checkout <分支名>

  (5) delete-branch : 刪除伺服器上的分支
      語法: gitlocal <專案> delete-branch <分支名>

  (6) merge         : 將來源分支合併至當前分支 (產生 Y 型匯流)
      語法: gitlocal <專案> merge <來源分支名>

 ☁️ [雲端與下載]
  (7) push          : 將伺服器進度推送到 GitHub
      語法: gitlocal <專案> push <遠端網址> <PAT_Token>

  (8) get-file      : 下載歷史紀錄中的「單一檔案」
      語法: gitlocal <專案> get-file <commit_sha> <檔案路徑> <存檔路徑>

  (9) zip           : 下載特定歷史點的「完整專案打包檔」
      語法: gitlocal <專案> zip <commit_sha> <儲存檔名.zip>
================================================================================`)
}

func main() {
	if len(os.Args) == 1 || (len(os.Args) == 2 && (os.Args[1] == "-h" || os.Args[1] == "--help")) {
		printUsage()
		os.Exit(0)
	}
	if len(os.Args) == 2 {
		fmt.Printf("❌ 錯誤：您指定了專案「%s」，但忘記輸入指令！\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}

	repoName := os.Args[1]
	command := os.Args[2]
	workDir, _ := os.Getwd()

	initServerURL()

	// 🌟 完整支援 9 大指令的路由分發
	switch command {
	case "status":
		commandStatus(repoName, workDir)
	case "commit":
		if len(os.Args) < 5 || os.Args[3] != "-m" {
			fmt.Println("❌ 語法錯誤。請使用: gitlocal <專案> commit -m \"說明\" <路徑>")
			return
		}
		commandCommit(repoName, os.Args[4], workDir)
	case "branch":
		if len(os.Args) < 4 {
			fmt.Println("❌ 語法錯誤。請使用: gitlocal <專案> branch <新分支名> [commit_sha]")
			return
		}
		commitSha := ""
		if len(os.Args) >= 5 {
			commitSha = os.Args[4]
		}
		commandBranch(repoName, os.Args[3], commitSha)
	case "checkout":
		if len(os.Args) < 4 {
			fmt.Println("❌ 語法錯誤。請使用: gitlocal <專案> checkout <分支名>")
			return
		}
		commandCheckout(repoName, os.Args[3])
	case "delete-branch":
		if len(os.Args) < 4 {
			fmt.Println("❌ 語法錯誤。請使用: gitlocal <專案> delete-branch <分支名>")
			return
		}
		commandDeleteBranch(repoName, os.Args[3])
	case "merge":
		if len(os.Args) < 4 {
			fmt.Println("❌ 語法錯誤。請使用: gitlocal <專案> merge <來源分支名>")
			return
		}
		commandMerge(repoName, os.Args[3])
	case "push":
		if len(os.Args) < 5 {
			fmt.Println("❌ 語法錯誤。請使用: gitlocal <專案> push <遠端網址> <PAT_Token>")
			return
		}
		commandPush(repoName, os.Args[3], os.Args[4])
	case "get-file":
		if len(os.Args) < 6 {
			fmt.Println("❌ 語法錯誤。請使用: gitlocal <專案> get-file <commit_sha> <檔案路徑> <存檔路徑>")
			return
		}
		commandGetFile(repoName, os.Args[3], os.Args[4], os.Args[5])
	case "zip":
		if len(os.Args) < 5 {
			fmt.Println("❌ 語法錯誤。請使用: gitlocal <專案> zip <commit_sha> <儲存檔名.zip>")
			return
		}
		commandZip(repoName, os.Args[3], os.Args[4])
	default:
		fmt.Printf("❌ 未知的指令：%s！請參考以下可用清單：\n\n", command)
		printUsage()
	}
}