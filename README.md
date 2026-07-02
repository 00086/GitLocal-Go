# 🗂️ GitLocal - 輕量級本地 Git 視覺化管理與高效能 CLI 生態系統 (Go 極速版)

GitLocal 是一個基於 **Go 語言 (Golang) 與 Gin 框架** 打造的極致輕量、無依賴（無需安裝系統 Git）的本地端 Git 倉庫管理系統。本專案不僅提供直覺、現代化的 Web 視覺化操作介面，更配備了專屬的 **高效能遠端指令列工具 (CLI Client)**，讓開發者在終端機與網頁之間無縫穿梭，享受流暢且極速的版本控制體驗。

---

## ✨ 核心特色 (Features)

### 1. 視覺化版本控制 (Web UI)

* **歷史時光機**：清晰的 Commit 紀錄列表，支援單一檔案或整個專案 (ZIP) 的歷史版本下載。
* **專業級 Diff 差異比對**：內建 Highlight.js 語法高亮，精準標示程式碼增刪（防複製 `+` `-` 符號設計），並完美適應深淺色主題。
* **平行宇宙 (分支系統)**：一鍵建立、切換、刪除分支，並支援「雙親節點合併 (Merge)」，在網頁上視覺化呈現分支匯流。
* **Git 樹狀圖**：整合 GitGraph.js，完美渲染出拓撲結構，直覺呈現複雜的分支交錯與歷史脈絡。
* 🌟 **全域深淺色模式 (Dark/Light Theme)**：一鍵無縫切換，支援 `localStorage` 狀態記憶。從首頁清單、差異比對、三大編輯器引擎到 GitHub 樣式的 Markdown 預覽，全系統自動完美對齊深淺色視覺體驗。
* 🌟 **自適應響應式視窗 (Smart Modals)**：全面採用 Flexbox 內部捲動架構，無論是長篇 Commit 紀錄、Git 樹狀圖還是發布上傳介面，皆能防呆防撐爆，確保最佳瀏覽操作體驗。

### 2. 強大的檔案管理、內建 IDE 與發布系統

* **全端檔案總管**：支援多層級資料夾瀏覽、拖曳式批次上傳 (Staging 暫存區預覽與單獨訊息撰寫)。
* 🌟 **三大編輯器引擎鼎立 (Tri-Engine IDE)**：內建 CodeMirror 6 (極速輕量)、Monaco (VS Code 同款強大高亮) 與 EasyMDE (專屬 Markdown 編輯器)。支援熱插拔無縫切換，編輯狀態與主題完美同步，網頁即是專業工作區。
* 🌟 **發布與標籤中心 (Releases & Tags)**：完整的發布管理系統。支援建立/編輯 Tag、支援 Markdown 撰寫發布說明 (Release Notes)，並允許拖曳上傳多個軟體封裝包與二進位附件檔案 (Assets)。
* **大檔案與二進位防護**：優雅攔截 Office 文件、圖片等特殊檔案，並內建 **LFS (Large File Storage)** 智慧攔截機制，將大檔隔離存放，提供安全備份與無損下載。

### 3. 🚀 遠端指令列工具 (CLI Client - gitlocal)

* **全功能遙控**：無須開啟網頁，即可在本地開發機直接對遠端伺服器下達 `status`、`commit`、`branch`、`checkout`、`merge`、`push` 等 9 大核心指令。
* **智慧層級化組態 (.gitlocal)**：支援專案級設定檔，自動對接遠端伺服器，擺脫硬編碼 IP 的災難。
* **多執行緒 (Goroutines) 極速運算**：在本地計算檔案 Hash時，自動啟動 Go 並發機制，就算是數千個檔案也能在毫秒級瞬間掃描完畢。

### 4. ⚡ 伺服器清單優先比對法 (極致效能優化)

* **顛覆傳統的 Diff 引擎**：本系統的 CLI 客戶端採用「伺服器清單主導」機制。掃描本地時，若發現檔案路徑不在遠端清單內，直接判定為新檔案，**絕對不讀取實體檔案、不浪費 CPU 計算雜湊值 (Hash)**。
* **極低硬碟 I/O 損耗**：只有當檔案確實存在於伺服器上，為了確認內容是否修改，才進行輕量雜湊計算，效能傲視群雄！

---

## 🛠️ 系統架構 (Architecture)

### 後端技術棧 (Backend Server)

* **核心框架**：[Gin](https://gin-gonic.com/) (Go 語言最受歡迎的高效能 Web 框架)
* **伺服器引擎**：Go 內建強大的 `net/http` (原生支援極高併發與多執行緒處理，無須外掛 WSGI 伺服器)
* **Git 底層引擎**：[go-git](https://github.com/go-git/go-git) (純 Go 實作的高度擴展性 Git 函式庫，**不依賴作業系統的 Git 執行檔**)
* **線程安全機制**：實作全域 `sync.Mutex` 互斥鎖，完美阻絕高併發寫入時可能造成的檔案庫崩潰。

### 客戶端技術棧 (CLI Client)

* **網路通訊**：Go 內建 `net/http` 與 `multipart` 表單封裝，提供極速的二進位串流傳輸。
* **參數解析**：Go 原生 `os.Args` 參數捕捉與自訂選單分發，零第三方依賴。
* **封裝工具**：Go 編譯器原生 `go build` (直接編譯為無環境依賴的跨平台原生單一執行檔)。

---

## 📂 專案結構 (Directory Structure)

```text
GitLocal/
├── main.go              # 伺服器主程式進入點 (環境與配置初始化)
├── routes.go            # 伺服器 API 路由與 Gin 框架控制器
├── database.go          # Git 核心操作邏輯 (go-git 封裝、LFS 與檔案系統操作)
├── gitlocal.go          # 遠端指令列工具 (CLI 客戶端腳本)
├── .gitignore           # 智慧忽略配置檔 (定義 CLI 與 Web 排除追蹤的垃圾檔案)
├── .gitlocal            # (使用者自建) 存放遠端伺服器 URL 的純文字設定檔
├── my_git_repos/        # (伺服器自動生成) 所有本地 Git 倉庫的存放地
└── templates/           # 前端畫面模板 (Go html/template 結合 Bootstrap)
    ├── index.html       # 首頁 (倉庫清單)
    ├── repo.html        # 倉庫主頁 (檔案總管、歷史紀錄、分支管理、發布 Releases)
    ├── edit.html        # 程式碼編輯器與 Markdown 預覽 (內建三大引擎)
    └── commit.html      # 歷史版本詳細差異 (Diff) 視窗

```

---

## 🚀 快速啟動 (Quick Start)

### 1. 啟動 GitLocal 伺服器 (B 電腦 / 伺服器端)

請確保伺服器端已安裝 **Go 1.20+**，在專案目錄下執行以下指令：

```bash
# 初始化模組與下載依賴 (首次執行)
go mod init gitlocal
go mod tidy
go get github.com/gin-gonic/gin
go get github.com/go-git/go-git/v5

# 啟動伺服器
go run .

```

打開瀏覽器，前往 `http://127.0.0.1:5001` 即可進入視覺化管理首頁。

### 2. 設定 CLI 客戶端 (A 電腦 / 開發機)

在您的實際開發專案資料夾根目錄下，建立一個名為 `.gitlocal` 的純文字檔，填入伺服器的實體網址：

```text
http://192.168.1.100:5001

```

*(未設定時，系統將自動安全退回本機 `http://127.0.0.1:5001` 作為基準。)*

---

## 💻 CLI 指令完全手冊 (CLI Reference)

以下範例假設您已將 `gitlocal` 編譯為系統環境變數中的執行檔。若未編譯，請使用 `go run gitlocal.go <專案名稱> <指令> [參數]`。

### 🛠️ [一、基礎操作]

#### 1. status (查看狀態)

純粹比對本地與伺服器的檔案差異狀態，**絕對不上傳任何資料**。

* **語法**：`gitlocal <專案> status`
* **範例**：`gitlocal my_repo status`

#### 2. commit (提交變更)

智慧比對差異並精準批次上傳。自動在網頁端留下多行異動履歷。

* **語法**：`gitlocal <專案> commit -m "<說明>" <檔案或.>`
* **範例**：`gitlocal my_repo commit -m "更新首頁佈局" .`

### 🌿 [二、分支管理]

#### 3. branch (建立分支)

建立平行宇宙。可選擇從最新進度建立，或指定 Commit SHA 進行歷史回溯建立。

* **語法**：`gitlocal <專案> branch <新分支名> [commit_sha]`
* **範例**：`gitlocal my_repo branch feature-api`

#### 4. checkout (切換分支)

遙控伺服器的工作目錄強制切換至指定分支。

* **語法**：`gitlocal <專案> checkout <分支名>`
* **範例**：`gitlocal my_repo checkout master`

#### 5. delete-branch (刪除分支)

永久刪除伺服器上的特定分支（無法刪除當前使用中的分支）。

* **語法**：`gitlocal <專案> delete-branch <分支名>`

#### 6. merge (合併分支)

將來源分支進度融合進當前分支，在樹狀圖上自動繪製出 Y 字型雙親節點。

* **語法**：`gitlocal <專案> merge <來源分支名>`

### ☁️ [三、雲端同步與時空下載]

#### 7. push (同步至 GitHub)

安全地將伺服器進度推送到 GitHub 倉庫。不留存 Token 於磁碟。

* **語法**：`gitlocal <專案> push <遠端網址> <PAT_Token>`
* **範例**：`gitlocal my_repo push https://github.com/user/repo.git ghp_123...`

#### 8. get-file (時空單檔下載)

抽取、下載某個歷史 Commit 版本的「單一檔案」到本地。

* **語法**：`gitlocal <專案> get-file <commit_sha> <伺服器路徑> <本地路徑>`

#### 9. zip (完整專案打包)

將指定歷史時間點的完整專案，打包為 ZIP 壓縮檔下載。

* **語法**：`gitlocal <專案> zip <commit_sha> <儲存檔名.zip>`

---

## 📦 獨立執行檔打包 (免 Go 環境發布)

Go 語言最強大的特色，就是能將程式碼編譯為**完全獨立的二進位執行檔 (Executable)**。開發機不需要安裝 Go 環境，也不需要任何相依套件，隨插即用！

### 1. 編譯 CLI 工具

在存放 `gitlocal.go` 的資料夾下打開終端機，執行以下指令：

**Windows 系統：**

```bash
go build -o gitlocal.exe gitlocal.go

```

**Linux / macOS 系統：**

```bash
go build -o gitlocal gitlocal.go

```

### 2. 環境變數全域設定 (盲打指令)

為了能在任何資料夾下直接打出 `gitlocal` 指令，請將編譯好的檔案加入系統路徑：

* **Windows**: 將生成的 `gitlocal.exe` 放入您的系統環境變數 `Path` 所包含的資料夾中（例如 `C:\Windows\System32\` 或者是自訂的 `C:\my_scripts\` 並加入 Path）。
* **Linux / macOS**: 賦予執行權限，並移入全域 `bin` 目錄：

```bash
chmod +x gitlocal
sudo mv gitlocal /usr/local/bin/

```

### 🌟 跨平台交叉編譯 (Cross-Compilation)

如果您在 Windows 上開發，想編譯給 Linux 伺服器使用，只需設定環境變數即可一鍵打包：

```bash
# 在 Windows 的 PowerShell 編譯 Linux 執行檔
$env:GOOS="linux"; $env:GOARCH="amd64"; go build -o gitlocal_linux gitlocal.go

```

---

## 📄 授權條款 (License)

本專案採用 **[MIT 授權條款](https://opensource.org/licenses/MIT)** 進行開源授權。您可以自由地複製、修改、分發及商業化使用本專案，唯須在所有副本中包含原作者的版權聲明與許可聲明。

```text
MIT License

Copyright (c) 2026 GitLocal Developer Team

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to person obtaining a copy of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

```
