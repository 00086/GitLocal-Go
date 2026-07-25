package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

type ResolveMergeRequest struct {
	SourceBranch  string            `json:"source_branch"`
	Message       string            `json:"message"`
	ResolvedFiles map[string]string `json:"resolved_files"`
}

func registerWebUI(r *gin.Engine) {

	// 🌟 全域首頁與建立專案
	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", gin.H{
			"repos": GetAllRepositories(),
		})
	})

	r.POST("/create_repo", func(c *gin.Context) {
		repoName := c.PostForm("repo_name")
		if repoName != "" {
			CreateNewRepo(repoName)
		}
		c.Redirect(http.StatusFound, "/")
	})
	
	// 🌟 新增：匯入專案 (ZIP 解壓即用)
	r.POST("/import_repo", func(c *gin.Context) {
		repoName := strings.TrimSpace(c.PostForm("repo_name"))
		fileHeader, err := c.FormFile("zip_file")
		if err != nil || repoName == "" {
			c.String(http.StatusBadRequest, "請填寫專案名稱並選擇要匯入的 ZIP 檔案")
			return
		}

		file, err := fileHeader.Open()
		if err != nil {
			c.String(http.StatusBadRequest, "無法讀取上傳檔案")
			return
		}
		defer file.Close()

		zipBytes, err := io.ReadAll(file)
		if err != nil {
			c.String(http.StatusInternalServerError, "讀取檔案失敗")
			return
		}

		if err := ImportRepoFromZip(repoName, zipBytes); err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}

		c.Redirect(http.StatusFound, "/repo/"+repoName)
	})

	// ==========================================
	// 🌟 1. 專案核心頁面與操作 Group ("/repo/:repo_name")
	// ==========================================
	repo := r.Group("/repo/:repo_name")
	{
		// 主詳細頁
		repo.GET("", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			details := GetRepoDetails(repoName)
			if details == nil {
				c.String(http.StatusNotFound, "找不到該倉庫")
				return
			}

			releases, _ := GetReleasesList(repoName)

			filesJson, _ := json.Marshal(details.Files)
			commitsJson, _ := json.Marshal(details.Commits)
			graphJson, _ := json.Marshal(details.GraphCommits)
			branchesJson, _ := json.Marshal(details.Branches)
			releasesJson, _ := json.Marshal(releases)

			c.HTML(http.StatusOK, "repo.html", gin.H{
				"repo_name":        repoName,
				"filesJson":        template.JS(filesJson),
				"commitsJson":      template.JS(commitsJson),
				"graphCommitsJson": template.JS(graphJson),
				"branchesJson":     template.JS(branchesJson),
				"releasesJson":     template.JS(releasesJson),
			})
		})

		// 批次上傳
		repo.POST("/batch_upload", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			message := c.PostForm("message")
			paths := c.PostFormArray("paths")

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
				
				targetPath := fileHeader.Filename
				if i < len(paths) && paths[i] != "" { targetPath = paths[i] }

				if err == nil {
					uploadFiles = append(uploadFiles, UploadFile{
						Path:  targetPath,
						Bytes: fileBytes,
					})
				}
			}

			if len(uploadFiles) > 0 {
				if err := BatchUploadAndCommit(repoName, uploadFiles, message); err != nil {
					c.String(http.StatusInternalServerError, err.Error())
					return
				}
			}
			c.String(http.StatusOK, "Success")
		})

		// 原始檔案讀取 (Raw)
		repo.GET("/raw/*file_path", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			filePath := c.Param("file_path") 
			c.File(filepath.Join(ReposDir, repoName, filePath))
		})

		// 分支控制
		repo.POST("/branch/create", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			bName := c.PostForm("branch_name")
			sha := c.PostForm("commit_sha")
			if bName != "" {
				if err := CreateBranch(repoName, bName, sha); err != nil {
					c.String(http.StatusInternalServerError, "建立分支失敗: "+err.Error())
					return
				}
				if err := SwitchBranch(repoName, bName); err != nil {
					c.String(http.StatusInternalServerError, "切換分支失敗: "+err.Error())
					return
				}
			}
			c.Redirect(http.StatusFound, "/repo/"+repoName)
		})

		repo.POST("/branch/switch", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			bName := c.PostForm("branch_name")
			if bName != "" { 
				if err := SwitchBranch(repoName, bName); err != nil {
					c.String(http.StatusInternalServerError, "切換分支失敗: "+err.Error())
					return
				}
			}
			c.Redirect(http.StatusFound, "/repo/"+repoName)
		})

		repo.POST("/branch/delete", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			bName := c.PostForm("branch_name")
			if bName != "" {
				if err := DeleteBranch(repoName, bName); err != nil {
					c.String(http.StatusBadRequest, err.Error())
					return
				}
			}
			c.Redirect(http.StatusFound, "/repo/"+repoName)
		})

		// Commit 差異檢視
		repo.GET("/commit/:commit_sha", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			sha := c.Param("commit_sha")

			diffData, err := GetCommitDiff(repoName, sha)
			if err != nil {
				c.String(http.StatusNotFound, "找不到該 Commit 紀錄")
				return
			}

			rawDiffs := diffData["diffs"].(string)
			diffLines := strings.Split(rawDiffs, "\n")
			fullMsg := diffData["message"].(string)
			msgParts := strings.SplitN(fullMsg, "\n", 2)
			commitTitle := msgParts[0]
			commitBody := ""
			if len(msgParts) > 1 { commitBody = strings.TrimSpace(msgParts[1]) }

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

		// 檔案編輯器 (對接 IDE 左側檔案樹)
		repo.GET("/file/*file_path", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			filePath := strings.TrimPrefix(c.Param("file_path"), "/")

			content, err := GetFileContent(repoName, filePath)
			if err != nil {
				c.String(http.StatusInternalServerError, "讀取檔案失敗: "+err.Error())
				return
			}

			// 🌟 新增：取得全專案檔案列表傳給 edit.html 側邊欄
			details := GetRepoDetails(repoName)
			filesJson := "[]"
			if details != nil {
				bytes, _ := json.Marshal(details.Files)
				filesJson = string(bytes)
			}

			c.HTML(http.StatusOK, "edit.html", gin.H{
				"repo_name": repoName,
				"file_path": filePath,
				"content":   content,
				"filesJson": template.JS(filesJson),
			})
		})

		repo.POST("/file/*file_path", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			filePath := strings.TrimPrefix(c.Param("file_path"), "/")
			content := c.PostForm("content")
			message := c.PostForm("message")
			if message == "" { message = "Update " + filePath }

			if err := WriteAndCommit(repoName, filePath, content, message); err != nil {
				c.String(http.StatusInternalServerError, "儲存變更失敗: "+err.Error())
				return
			}
			c.Redirect(http.StatusFound, "/repo/"+repoName)
		})

		// 下載與操作 API
		repo.GET("/download/:commit_sha/*file_path", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			sha := c.Param("commit_sha")
			filePath := strings.TrimPrefix(c.Param("file_path"), "/")

			fileBytes, timeStr, err := GetFileAtCommit(repoName, sha, filePath)
			if err != nil {
				c.String(http.StatusNotFound, "找不到該歷史版本的檔案")
				return
			}
			parts := strings.Split(filePath, "/")
			pureFileName := parts[len(parts)-1]
			customDownloadName := fmt.Sprintf("%s_%s_%s", timeStr, sha[:7], pureFileName)

			c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", customDownloadName))
			c.Data(http.StatusOK, "application/octet-stream", fileBytes)
		})

		repo.GET("/download_zip/:commit_sha", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			sha := c.Param("commit_sha")

			zipBytes, err := GetCommitZip(repoName, sha)
			if err != nil {
				c.String(http.StatusInternalServerError, "打包 ZIP 失敗: "+err.Error())
				return
			}
			c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s_%s.zip", repoName, sha[:7]))
			c.Data(http.StatusOK, "application/zip", zipBytes)
		})

		repo.POST("/move", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			oldPath := c.PostForm("old_path")
			newPath := c.PostForm("new_path")
			if strings.Contains(newPath, "..") {
				c.String(http.StatusBadRequest, "無效的目標路徑")
				return
			}
			if err := MoveAndCommit(repoName, oldPath, newPath); err != nil {
				c.String(http.StatusInternalServerError, err.Error())
				return
			}
			c.String(http.StatusOK, "Success")
		})
		
		// 🌟 新增：一鍵撤銷特定 Commit API
		repo.POST("/revert/:commit_sha", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			sha := c.Param("commit_sha")
			if err := RevertCommit(repoName, sha); err != nil {
				c.String(http.StatusInternalServerError, "撤銷失敗: "+err.Error())
				return
			}
			c.String(http.StatusOK, "Success")
		})

		repo.POST("/push", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			remoteURL := strings.TrimSpace(c.PostForm("remote_url"))
			token := strings.TrimSpace(c.PostForm("token"))
			if remoteURL == "" || token == "" {
				c.String(http.StatusBadRequest, "必須提供 GitHub 網址與安全 Token")
				return
			}
			if err := PushToRemote(repoName, remoteURL, token); err != nil {
				c.String(http.StatusInternalServerError, err.Error())
				return
			}
			c.String(http.StatusOK, "Success")
		})

		repo.POST("/delete/*file_path", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			filePath := strings.TrimPrefix(c.Param("file_path"), "/")
			if filePath == "" {
				c.String(http.StatusBadRequest, "必須指定要刪除的路徑")
				return
			}
			if err := DeleteFileAndCommit(repoName, filePath); err != nil {
				c.String(http.StatusInternalServerError, "刪除失敗: "+err.Error())
				return
			}
			c.Redirect(http.StatusFound, "/repo/"+repoName)
		})

		// Releases API
		repo.POST("/release/create", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			tagName := c.PostForm("tag_name")
			message := c.PostForm("message")

			_ = c.Request.ParseMultipartForm(100 << 20)
			var uploadAssets []UploadFile
			if c.Request.MultipartForm != nil {
				for _, fileHeader := range c.Request.MultipartForm.File["assets"] {
					if file, err := fileHeader.Open(); err == nil {
						fileBytes, _ := io.ReadAll(file)
						uploadAssets = append(uploadAssets, UploadFile{Path: fileHeader.Filename, Bytes: fileBytes})
						file.Close()
					}
				}
			}

			if tagName != "" {
				if err := CreateRelease(repoName, tagName, message, uploadAssets); err != nil {
					c.String(http.StatusInternalServerError, "建立發布失敗: "+err.Error())
					return
				}
			}
			c.Redirect(http.StatusFound, "/repo/"+repoName)
		})

		repo.GET("/release/download/:tag_name/*file_name", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			tagName := c.Param("tag_name")
			fileName := strings.TrimPrefix(c.Param("file_name"), "/")
			targetPath := filepath.Join(ReposDir, repoName, ".gitlocal", "releases", tagName, fileName)

			if _, err := os.Stat(targetPath); os.IsNotExist(err) {
				c.String(http.StatusNotFound, "找不到該發布檔案金庫")
				return
			}
			c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(fileName)))
			c.File(targetPath)
		})

		repo.POST("/release/delete/:tag_name", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			tagName := c.Param("tag_name")
			if tagName != "" {
				if err := DeleteRelease(repoName, tagName); err != nil {
					c.String(http.StatusInternalServerError, "刪除發布失敗: "+err.Error())
					return
				}
			}
			c.Redirect(http.StatusFound, "/repo/"+repoName)
		})

		repo.POST("/release/edit/:tag_name", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			oldTagName := c.Param("tag_name")
			newTagName := c.PostForm("new_tag_name")
			message := c.PostForm("message")
			if newTagName == "" { newTagName = oldTagName }

			_ = c.Request.ParseMultipartForm(100 << 20)
			var uploadAssets []UploadFile
			if c.Request.MultipartForm != nil {
				for _, fileHeader := range c.Request.MultipartForm.File["assets"] {
					if file, err := fileHeader.Open(); err == nil {
						fileBytes, _ := io.ReadAll(file)
						uploadAssets = append(uploadAssets, UploadFile{Path: fileHeader.Filename, Bytes: fileBytes})
						file.Close()
					}
				}
			}

			if oldTagName != "" {
				if err := EditRelease(repoName, oldTagName, newTagName, message, uploadAssets); err != nil {
					c.String(http.StatusInternalServerError, "編輯發布失敗: "+err.Error())
					return
				}
			}
			c.Redirect(http.StatusFound, "/repo/"+repoName)
		})

		// 3-Way Merge API
		repo.POST("/branch/merge", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			sourceBranch := c.PostForm("source_branch")
			conflicts, err := InitiateMerge(repoName, sourceBranch)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"conflicts": conflicts})
		})

		repo.POST("/branch/merge/resolve", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			var req ResolveMergeRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.String(http.StatusBadRequest, "參數解析失敗: "+err.Error())
				return
			}
			if err := FinalizeMerge(repoName, req.SourceBranch, req.Message, req.ResolvedFiles); err != nil {
				c.String(http.StatusInternalServerError, "合併寫入失敗: "+err.Error())
				return
			}
			c.String(http.StatusOK, "Success")
		})
		
		// 🌟 新增：下載完整 Git 歷史備份包 (.zip)
		repo.GET("/download_full_backup", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			zipBytes, err := GetFullRepoZip(repoName)
			if err != nil {
				c.String(http.StatusInternalServerError, "備份失敗: "+err.Error())
				return
			}
			c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s_full_backup.zip", repoName))
			c.Data(http.StatusOK, "application/zip", zipBytes)
		})
	}

	// ==========================================
	// 🌟 2. 非同步數據 API Group ("/api/repo/:repo_name")
	// ==========================================
	api := r.Group("/api/repo/:repo_name")
	{
		api.GET("/manifest", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			manifest, err := GetRepoManifest(repoName)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, manifest)
		})

		api.GET("/commits", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			pageStr := c.DefaultQuery("page", "1")
			limitStr := c.DefaultQuery("limit", "10") // 🌟 改為預設 10 筆
			
			page, limit := 1, 10 // 🌟 後端預設每頁 10 筆
			fmt.Sscanf(pageStr, "%d", &page)
			fmt.Sscanf(limitStr, "%d", &limit)

			resp, err := GetPaginatedCommits(repoName, page, limit)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, resp)
		})

		// 🌟 新增：背景異步補抓檔案「最後修改 Commit」API (GitHub 模式)
		api.GET("/file-commits", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			fileDetails, err := GetFileCommitsDetails(repoName)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, fileDetails)
		})

		// 🌟 新增：點擊按鈕時按需載入 Git 樹狀圖 API
		api.GET("/git-graph", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			graphData, err := GetGitGraphData(repoName)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, graphData)
		})
		
		// 🌟 新增：手動刷新/清空指定倉庫的記憶體快取 API
		api.POST("/cache/clear", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			invalidateRepoCache(repoName)
			c.JSON(http.StatusOK, gin.H{"status": "success", "message": "已清除 " + repoName + " 的記憶體快取"})
		})
		
		// 🌟 新增：健康診斷與瘦身 API
		api.GET("/health", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			health, err := GetRepoHealth(repoName)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, health)
		})

		api.POST("/gc", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			out, err := RunGitGC(repoName)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "output": out})
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": "success", "message": "垃圾回收瘦身完成！", "output": out})
		})

		// 🌟 新增：Web 輕量級命令列終端 API
		api.POST("/terminal", func(c *gin.Context) {
			repoName := c.Param("repo_name")
			cmdStr := c.PostForm("command")
			output, err := RunWebTerminalCmd(repoName, cmdStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"output": output})
		})
	}
	
}