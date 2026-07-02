package main

import (
	"fmt"
	"net/http"
	"html/template" // 🌟 新增這個套件
	"encoding/json"
	"io"
	"strings" // 🌟 補上這行，拯救 undefined 錯誤！
	"os"             // 🌟 補上這行
	"path/filepath"  // 🌟 補上這行

	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------
// 註冊所有路由
// ---------------------------------------------------------
func registerWebUI(r *gin.Engine) {

	// 🌟 1. 首頁 (對應 Python 的 index)
	r.GET("/", func(c *gin.Context) {
		// 🌟 呼叫真實的 database 函數
		realRepos := GetAllRepositories() 
		c.HTML(http.StatusOK, "index.html", gin.H{
			"repos": realRepos,
		})
	})

	// 🌟 替換 2. 建立新倉庫
	r.POST("/create_repo", func(c *gin.Context) {
		repoName := c.PostForm("repo_name")
		if repoName != "" {
			CreateNewRepo(repoName) // 呼叫真實寫入函數
		}
		c.Redirect(http.StatusFound, "/")
	})

	// 🌟 3. 專案詳細頁面 (對應 Python 的 repo_detail)
	r.GET("/repo/:repo_name", func(c *gin.Context) {
		repoName := c.Param("repo_name")

		// 🌟 呼叫真實引擎取得資料庫細節
		details := GetRepoDetails(repoName)
		if details == nil {
			c.String(http.StatusNotFound, "找不到該倉庫")
			return
		}

		// 🌟 讀取 Releases 金庫資料
		releases, _ := GetReleasesList(repoName)

		// 將 Go 的結構體轉換為 JSON 字串給前端 JavaScript 使用
		filesJson, _ := json.Marshal(details.Files)
		commitsJson, _ := json.Marshal(details.Commits)
		graphJson, _ := json.Marshal(details.GraphCommits)
		branchesJson, _ := json.Marshal(details.Branches)
		releasesJson, _ := json.Marshal(releases) // 🌟 打包 Release 資料

		c.HTML(http.StatusOK, "repo.html", gin.H{
			"repo_name":        repoName,
			"filesJson":        template.JS(filesJson),
			"commitsJson":      template.JS(commitsJson),
			"graphCommitsJson": template.JS(graphJson),
			"branchesJson":     template.JS(branchesJson),
			"releasesJson":     template.JS(releasesJson), // 🌟 傳遞給前端
		})
	})

	// 🌟 4. CLI 專用 API: 獲取專案清單 (對應 Python 的 api_manifest)
	r.GET("/api/repo/:repo_name/manifest", func(c *gin.Context) {
		repoName := c.Param("repo_name")
		
		manifest, err := GetRepoManifest(repoName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		
		c.JSON(http.StatusOK, manifest)
	})

	// 🌟 替換 5. 批次上傳 (接收拖曳檔案並保留目錄結構)
	r.POST("/repo/:repo_name/batch_upload", func(c *gin.Context) {
		repoName := c.Param("repo_name")
		message := c.PostForm("message")
		paths := c.PostFormArray("paths") // 🌟 新增：接收前端明確傳遞的完整相對路徑數組

		form, err := c.MultipartForm()
		if err != nil {
			c.String(http.StatusBadRequest, "解析表單失敗")
			return
		}

		var uploadFiles []UploadFile
		files := form.File["files"]
		
		for i, fileHeader := range files {
			file, err := fileHeader.Open()
			if err != nil { continue }
			
			fileBytes, err := io.ReadAll(file)
			file.Close()
			
			// 🌟 核心修正：優先使用前端傳來的真實相對路徑 (如 templates/commit.html)
			// 若長度不對稱則降級使用 fileHeader.Filename
			targetPath := fileHeader.Filename
			if i < len(paths) && paths[i] != "" {
				targetPath = paths[i]
			}

			if err == nil {
				uploadFiles = append(uploadFiles, UploadFile{
					Path:  targetPath,
					Bytes: fileBytes,
				})
			}
		}

		if len(uploadFiles) > 0 {
			err := BatchUploadAndCommit(repoName, uploadFiles, message)
			if err != nil {
				c.String(http.StatusInternalServerError, err.Error())
				return
			}
		}
		c.String(http.StatusOK, "Success")
	})
	
	// 🌟 6. 取得原始檔案 (供圖片預覽或 README 使用，對應 raw_file)
	// 注意：Gin 的 *file_path 會捕捉斜線後面的所有路徑
	r.GET("/repo/:repo_name/raw/*file_path", func(c *gin.Context) {
		repoName := c.Param("repo_name")
		filePath := c.Param("file_path") 

		// 設定您的本地 Git 倉庫存放資料夾 (對應 Python 的 REPOS_DIR)
		// 假設您的專案資料夾放在目前目錄下的 my_git_repos
		reposDir := "./my_git_repos" 
		
		// 組合出實體檔案的絕對/相對路徑 (去除 filePath 開頭可能多出的斜線)
		fullPath := reposDir + "/" + repoName + filePath
		
		// c.File 會自動處理 MIME type 並安全地回傳實體檔案內容給前端
		c.File(fullPath)
	})
	
	// 🌟 新增：分支建立與切換 (合併在同一個路由邏輯，與您原本 Python 前端設計對接)
	r.POST("/repo/:repo_name/branch/create", func(c *gin.Context) {
		repoName := c.Param("repo_name")
		branchName := c.PostForm("branch_name")
		commitSha := c.PostForm("commit_sha")

		if branchName != "" {
			CreateBranch(repoName, branchName, commitSha)
			SwitchBranch(repoName, branchName) // 建立後自動切換過去
		}
		c.Redirect(http.StatusFound, "/repo/"+repoName)
	})

	r.POST("/repo/:repo_name/branch/switch", func(c *gin.Context) {
		repoName := c.Param("repo_name")
		branchName := c.PostForm("branch_name")
		if branchName != "" {
			SwitchBranch(repoName, branchName)
		}
		c.Redirect(http.StatusFound, "/repo/"+repoName)
	})

	r.POST("/repo/:repo_name/branch/delete", func(c *gin.Context) {
		repoName := c.Param("repo_name")
		branchName := c.PostForm("branch_name")
		if branchName != "" {
			err := DeleteBranch(repoName, branchName)
			if err != nil {
				c.String(http.StatusBadRequest, err.Error())
				return
			}
		}
		c.Redirect(http.StatusFound, "/repo/"+repoName)
	})
	
	// 🌟 7. Commit 詳細紀錄差異頁面 (對應 Python 的 commit_detail)
	r.GET("/repo/:repo_name/commit/:commit_sha", func(c *gin.Context) {
		repoName := c.Param("repo_name")
		commitSha := c.Param("commit_sha")

		diffData, err := GetCommitDiff(repoName, commitSha)
		if err != nil {
			c.String(http.StatusNotFound, "找不到該 Commit 紀錄")
			return
		}

		// 💡 在後端把 Diff 文字按換行切開，直接做成陣列交給前端
		rawDiffs := diffData["diffs"].(string)
		diffLines := strings.Split(rawDiffs, "\n")

		// 💡 在後端處理 Commit 備註的切分 (第一行為標題，其餘為詳細備註)
		fullMsg := diffData["message"].(string)
		msgParts := strings.SplitN(fullMsg, "\n", 2)
		commitTitle := msgParts[0]
		commitBody := ""
		if len(msgParts) > 1 {
			commitBody = strings.TrimSpace(msgParts[1])
		}

		c.HTML(http.StatusOK, "commit.html", gin.H{
			"repo_name":   repoName,
			"hexsha":      diffData["hexsha"],
			"author":      diffData["author"],
			"time":        diffData["time"],
			"commitTitle": commitTitle,
			"commitBody":  commitBody,
			"diffLines":   diffLines,
			"rawDiffs":    rawDiffs,
		})
	})

	// 🌟 8. 檔案編輯器路由：支援 GET 讀取與 POST 儲存 (對應 Python 的 edit_file)
	// 使用 Gin 的 *file_path 語法捕捉包含多層資料夾的完整相對路徑
	r.GET("/repo/:repo_name/file/*file_path", func(c *gin.Context) {
		repoName := c.Param("repo_name")
		filePath := strings.TrimPrefix(c.Param("file_path"), "/")

		content, err := GetFileContent(repoName, filePath)
		if err != nil {
			c.String(http.StatusInternalServerError, "讀取檔案失敗: "+err.Error())
			return
		}

		c.HTML(http.StatusOK, "edit.html", gin.H{
			"repo_name": repoName,
			"file_path": filePath,
			"content":   content,
		})
	})

	r.POST("/repo/:repo_name/file/*file_path", func(c *gin.Context) {
		repoName := c.Param("repo_name")
		filePath := strings.TrimPrefix(c.Param("file_path"), "/")
		
		content := c.PostForm("content")
		message := c.PostForm("message")

		if message == "" {
			message = "Update " + filePath
		}

		err := WriteAndCommit(repoName, filePath, content, message)
		if err != nil {
			c.String(http.StatusInternalServerError, "儲存變更失敗: "+err.Error())
			return
		}

		// 儲存成功後，自動導回該專案的主詳細頁面
		c.Redirect(http.StatusFound, "/repo/"+repoName)
	})
	
	// 🌟 9. 下載特定 Commit 版本的單一檔案 (對應 Python 的 download_file_at_commit)
	r.GET("/repo/:repo_name/download/:commit_sha/*file_path", func(c *gin.Context) {
		repoName := c.Param("repo_name")
		commitSha := c.Param("commit_sha")
		filePath := strings.TrimPrefix(c.Param("file_path"), "/")

		fileBytes, timeStr, err := GetFileAtCommit(repoName, commitSha, filePath)
		if err != nil {
			c.String(http.StatusNotFound, "找不到該歷史版本的檔案")
			return
		}

		// 分離出純檔名
		parts := strings.Split(filePath, "/")
		pureFileName := parts[len(parts)-1]

		// 組合 Python 版的貼心自訂下載檔名： [歷史時間]_[簡短SHA]_[原檔名]
		customDownloadName := fmt.Sprintf("%s_%s_%s", timeStr, commitSha[:7], pureFileName)

		// 宣告為強迫瀏覽器下載的 attachment
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", customDownloadName))
		c.Data(http.StatusOK, "application/octet-stream", fileBytes)
	})

	// 🌟 10. 打包特定 Commit 時空點的專案 ZIP 下載 (對應 Python 的 download_commit_zip)
	r.GET("/repo/:repo_name/download_zip/:commit_sha", func(c *gin.Context) {
		repoName := c.Param("repo_name")
		commitSha := c.Param("commit_sha")

		zipBytes, err := GetCommitZip(repoName, commitSha)
		if err != nil {
			c.String(http.StatusInternalServerError, "打包 ZIP 失敗: "+err.Error())
			return
		}

		customZipName := fmt.Sprintf("%s_%s.zip", repoName, commitSha[:7])
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", customZipName))
		c.Data(http.StatusOK, "application/zip", zipBytes)
	})

	// 🌟 11. 接收網頁滑鼠拖曳改名/移動檔案請求 (對應 Python 的 move_file)
	r.POST("/repo/:repo_name/move", func(c *gin.Context) {
		repoName := c.Param("repo_name")
		oldPath := c.PostForm("old_path")
		newPath := c.PostForm("new_path")

		if strings.Contains(newPath, "..") {
			c.String(http.StatusBadRequest, "無效的目標路徑防禦")
			return
		}

		err := MoveAndCommit(repoName, oldPath, newPath)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		c.String(http.StatusOK, "Success")
	})

	// 🌟 12. 接收網頁一鍵安全 Push 到遠端 GitHub 請求 (對應 Python 的 push_repo)
	r.POST("/repo/:repo_name/push", func(c *gin.Context) {
		repoName := c.Param("repo_name")
		remoteURL := c.PostForm("remote_url")
		token := c.PostForm("token")

		if remoteURL == "" || token == "" {
			c.String(http.StatusBadRequest, "必須提供 GitHub 網址與安全 Token")
			return
		}

		err := PushToRemote(repoName, strings.TrimSpace(remoteURL), strings.TrimSpace(token))
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		c.String(http.StatusOK, "Success")
	})
	
	// 🌟 13. 刪除特定檔案並自動 Commit
	r.POST("/repo/:repo_name/delete/*file_path", func(c *gin.Context) {
		repoName := c.Param("repo_name")
		// 使用 TrimPrefix 去除 * 捕捉時最前面多出的斜線
		filePath := strings.TrimPrefix(c.Param("file_path"), "/")

		if filePath == "" {
			c.String(http.StatusBadRequest, "必須指定要刪除的檔案路徑")
			return
		}

		err := DeleteFileAndCommit(repoName, filePath)
		if err != nil {
			c.String(http.StatusInternalServerError, "刪除失敗: "+err.Error())
			return
		}

		// 成功刪除後，將網頁重新導向回該倉庫的主畫面
		c.Redirect(http.StatusFound, "/repo/"+repoName)
	})
	
	// 🌟 ==========================================
	// 🌟 Releases 發布中心專屬 API
	// 🌟 ==========================================

	// 新增：建立 Release (打標籤 + 封存手動上傳的檔案)
	r.POST("/repo/:repo_name/release/create", func(c *gin.Context) {
		repoName := c.Param("repo_name")
		tagName := c.PostForm("tag_name")
		message := c.PostForm("message")

		// 🌟 解析上傳的檔案 (設定最大記憶體 100MB)
		err := c.Request.ParseMultipartForm(100 << 20)
		var uploadAssets []UploadFile
		
		if err == nil && c.Request.MultipartForm != nil {
			files := c.Request.MultipartForm.File["assets"]
			for _, fileHeader := range files {
				file, err := fileHeader.Open()
				if err == nil {
					fileBytes, _ := io.ReadAll(file)
					uploadAssets = append(uploadAssets, UploadFile{
						Path:  fileHeader.Filename,
						Bytes: fileBytes,
					})
					file.Close()
				}
			}
		}

		if tagName != "" {
			// 🌟 將檔案陣列傳給 CreateRelease
			err := CreateRelease(repoName, tagName, message, uploadAssets)
			if err != nil {
				c.String(http.StatusInternalServerError, "建立發布失敗: "+err.Error())
				return
			}
		}
		c.Redirect(http.StatusFound, "/repo/"+repoName)
	})

	// 新增：下載 Release 專屬金庫內的檔案 (包含 .exe 與源碼 .zip)
	r.GET("/repo/:repo_name/release/download/:tag_name/*file_name", func(c *gin.Context) {
		repoName := c.Param("repo_name")
		tagName := c.Param("tag_name")
		// Gin 的 * 捕捉會帶有前綴斜線，需清除
		fileName := strings.TrimPrefix(c.Param("file_name"), "/")

		// 指向專屬金庫的實體路徑
		targetPath := filepath.Join(ReposDir, repoName, ".gitlocal", "releases", tagName, fileName)
		
		if _, err := os.Stat(targetPath); os.IsNotExist(err) {
			c.String(http.StatusNotFound, "找不到該發布檔案金庫")
			return
		}

		// 強制瀏覽器觸發下載，而非在瀏覽器內開啟
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(fileName)))
		c.File(targetPath)
	})
	
	// 新增：刪除特定的 Release 版本 (連帶拔除標籤與金庫)
	r.POST("/repo/:repo_name/release/delete/:tag_name", func(c *gin.Context) {
		repoName := c.Param("repo_name")
		tagName := c.Param("tag_name")

		if tagName != "" {
			err := DeleteRelease(repoName, tagName)
			if err != nil {
				c.String(http.StatusInternalServerError, "刪除發布失敗: "+err.Error())
				return
			}
		}
		// 刪除成功後，自動導回專案主畫面重整
		c.Redirect(http.StatusFound, "/repo/"+repoName)
	})
	
	// 🌟 升級：編輯特定 Release (接收新舊標籤)
	r.POST("/repo/:repo_name/release/edit/:tag_name", func(c *gin.Context) {
		repoName := c.Param("repo_name")
		oldTagName := c.Param("tag_name")
		newTagName := c.PostForm("new_tag_name") // 接收前端傳來的新標籤名
		message := c.PostForm("message")

		// 防呆：如果前端沒有傳新名字，就維持原名
		if newTagName == "" {
			newTagName = oldTagName
		}

		err := c.Request.ParseMultipartForm(100 << 20)
		var uploadAssets []UploadFile
		
		if err == nil && c.Request.MultipartForm != nil {
			files := c.Request.MultipartForm.File["assets"]
			for _, fileHeader := range files {
				file, err := fileHeader.Open()
				if err == nil {
					fileBytes, _ := io.ReadAll(file)
					uploadAssets = append(uploadAssets, UploadFile{
						Path:  fileHeader.Filename,
						Bytes: fileBytes,
					})
					file.Close()
				}
			}
		}

		if oldTagName != "" {
			// 🌟 傳入新、舊標籤名稱給底層引擎
			err := EditRelease(repoName, oldTagName, newTagName, message, uploadAssets)
			if err != nil {
				c.String(http.StatusInternalServerError, "編輯發布失敗: "+err.Error())
				return
			}
		}
		c.Redirect(http.StatusFound, "/repo/"+repoName)
	})	
	// TODO: 陸續補齊其他 API (download, branch/create 等)
}
