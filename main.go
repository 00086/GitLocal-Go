package main

import (
	"embed"          // 🌟 1. 引入內嵌檔案套件
	"fmt"
	"html/template"  // 🌟 2. 引入標準庫的 HTML 模板套件
	"github.com/gin-gonic/gin"
)

// 🌟 3. 核心魔法：這行註解是給 Go 編譯器看的指令！
// 它會告訴編譯器，在打包時把整個 templates 資料夾塞進二進位執行檔中，並掛載到 templateFS 變數上。
//go:embed templates/*
var templateFS embed.FS

func createApp() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// ❌ 原本的寫法（依賴外部硬碟的實體資料夾，換地方就崩潰）：
	// r.LoadHTMLGlob("templates/*.html")

	// ✅ 升級為內嵌檔案寫法（網頁已經被鎖在 .exe 體內，完全不需要外部資料夾了！）：
	templ := template.Must(template.ParseFS(templateFS, "templates/*.html"))
	r.SetHTMLTemplate(templ)

	registerWebUI(r)
	return r
}

func main() {
	app := createApp()

	fmt.Println("🚀 生產級 Go 原生內嵌網頁伺服器啟動中...")
	fmt.Println("👉 本地管理網址: http://127.0.0.1:5001")

	err := app.Run("0.0.0.0:5001")
	if err != nil {
		fmt.Printf("❌ 伺服器啟動失敗: %v\n", err)
	}
}