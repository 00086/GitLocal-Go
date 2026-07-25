# 🗂️ GitLocal - 輕量級本地 Git 視覺化管理與極速 IDE 生態系統 (Go 原生極速版)

`GitLocal` 是一個基於 **Go 語言 (Golang) 與 Gin 框架** 打造的極致輕量、零依賴（無需安裝系統 Git 亦可 100% 獨立運行）的本地端 Git 倉庫管理與 IDE 整合系統。

本專案不僅提供雙欄式現代化 Web IDE 介面，更配備專屬的 **高效能遠端指令列工具 (CLI Client - gitlocal)**，完整支援 **原生 Git 雙語法（自動抓當前目錄 vs 指定專案）**、**3-Way 視覺化合併**、**一鍵歷史撤銷 (Revert)**、**雙軌 GC 垃圾回收瘦身**、**完整金庫備份與智慧 ZIP 匯入**及 **Web 輕量終端機**，讓開發者在網頁與終端機之間體驗無縫、流暢的版本控制。

---

## ✨ 核心特色與功能 (Features)

### 1. 🚀 遠端指令列工具 (CLI Client - gitlocal.go)

* 🌟 **智慧雙語法結構 (Dual Syntax Support)**：
* **【方法 A】預設 (自動抓當前目錄)**：直接下達 `gitlocal status`，系統自動捕捉現行資料夾名稱作為專案名稱。


* **【方法 B】指定特定專案名稱**：支援 `gitlocal GitLocal-Go status`，可跨目錄遙控指定專案。




* **9 大全功能遙控**：完整支援 `status`、`commit`、`switch / checkout`、`branch`（不帶參數自動列出並高亮當前分支）、`delete-branch`、`merge`、`push`、`get-file`、`zip` 等核心指令。


* **智慧層級化組態 (.gitlocal)**：支援專案級設定檔，自動對接遠端伺服器。


* **多執行緒 (Goroutines) 極速運算**：在本地計算檔案 Hash 時，自動啟動 Go 並發機制（含信號量管制），數千個檔案能在毫秒級瞬間掃描完畢。


* **伺服器清單優先比對法 (極致效能優化)**：CLI 客戶端採用「伺服器清單主導」機制。掃描本地時，若發現檔案路徑不在遠端清單內，直接判定為新檔案，**絕對不讀取實體檔案、不浪費 CPU 計算雜湊值 (Hash)**。只有檔案存在時才進行輕量雜湊，硬碟 I/O 損耗極低！



### 2. 🎨 現代化雙欄 IDE 工作區 (Web IDE Workspace)

* **雙欄左側檔案樹 (`#sidebar`)**：自動將所有專案解析為 GitHub 風格的階層式樹狀目錄，點擊檔案即可在右側無縫開啟專屬編輯器。
* **首頁大改版 (UX 重構)**：建立新倉庫與匯入專案拆分為獨立雙卡片，畫面寬敞清晰，大幅提升操作體驗。
* **全域深淺色模式 (Dark/Light Theme)**：一鍵無縫切換，支援 `localStorage` 狀態記憶與 Markdown 預覽同步切換。
* **自適應彈性視窗 (Smart Modals)**：採用 Flexbox 內部滾動，確保 Commit 詳細比對、Git 樹狀圖及發布上傳視窗防撐爆、不跑版。

### 3. ⚡ 高效能記憶體快取與即時相對時間 (In-Memory Cache Engine)

* **Sub-millisecond 極速回應**：內建 `repoCache` 記憶體快取機制，專案結構、Git 樹狀圖與歷史紀錄由 RAM 瞬間載入。
* **寫入自動失效 (Auto Invalidating)**：任何 Commit、分支切換或檔案異動皆會自動清空該專案快取，保證資料零延遲同步。
* **前端動態時間計算 (無快取凍結)**：後端統一存儲 `2006-01-02 15:04:05` 絕對時間，由瀏覽器 JavaScript 當下即時計算「幾分鐘前」，徹底解決記憶體快取的殘影問題。

### 4. 🧹 雙軌 GC 垃圾回收瘦身與金庫診斷 (Dual-Track GC & Health)

* **即時金庫容量診斷**：主介面頂部顯示當前 `.git` 金庫的硬碟佔用大小。
* **雙軌適應瘦身引擎**：
* **有裝系統 Git**：自動呼叫 `git gc --prune=now` 進行深度壓縮與 Packfile 重組。
* **未裝系統 Git (純 Go 模式)**：自動切換至 **Go 原生垃圾回收演算法**，自動掃描並清理孤立廢棄物件 (Unreachable Objects) 與 `.lock` 暫存檔，實現 100% 零依賴運作！



### 5. ↺ 一鍵歷史撤銷 (Revert Commit Engine)

* **安全反向修復**：在 Commit 歷史紀錄列表中，點擊黃色 **`↺ 撤銷`** 按鈕，系統會自動比對變更並建立一個「反向修復提交」，安全抵銷該次變更，**絕對不破壞或丟失專案的歷史時間線**。

### 6. 📥 智慧專案匯入與完整金庫備份 (Smart Import & Full Backup)

* **完整金庫備份 (`💾 備份金庫`)**：一鍵將專案資料夾與隱藏 `.git` 金庫整包下載為 Zip，保留所有分支、Commit 與 Tag 歷史。
* **單檔與快照下載 (`📦 ZIP`)**：可下載任何歷史 Commit 時間點的乾淨程式碼快照。
* **智慧雙模式匯入 (Drop Zone)**：首頁支援**點擊瀏覽選擇按鈕**與**滑鼠拖曳 `.zip**` 兩種方式：
* **匯入 Full Backup ZIP**：自動識別 `.git`，100% 還原所有分支與 Commit 歷史。
* **匯入純源碼 ZIP**：自動執行 `git init` 並完成第一次初始提交 (Initial import)。



### 7. ⚔️ 視覺化 3-Way Merge 與衝突解決

* **Monaco Side-by-Side 對比引擎**：內建 VS Code 同款的 Monaco Diff Editor，雙視窗並排呈現當前分支 (Ours) 與目標分支 (Theirs) 的程式碼差異。
* **線上即時解衝突**：直接在右側視窗修訂程式碼，一鍵儲存並生成漂亮的 Y 字型雙親合併 Commit。

### 8. 🖥️ 內建安全 Web 終端機 (Web Terminal Drawer)

* **底部折疊終端機**：點擊 `🖥️ 終端機` 即可在網頁底部展開全功能 Console。
* **白名單安全限制**：僅允許執行 `git` 與 `gitlocal` 指令。
* **ANSI 彩色碼轉譯**：自動將命令行輸出的顏色碼轉譯為彩色 HTML，體驗如原生地端 Console。

### 9. 📝 內建 IDE 與發布標籤中心 (Releases & Tags)

* **三大編輯器引擎鼎立**：`edit.html` 整合 Monaco Editor、CodeMirror 6 與 EasyMDE (Markdown)。
* **發布中心 (Releases)**：支援打包 Tag、Markdown 撰寫發布日誌，並可上傳多個 `.exe` 或 `.zip` 二進位封裝資產。
* **LFS 大檔案隔離**：超過 5MB 的二進位檔案自動轉為 LFS Pointer 隔離儲存，保護 Git 庫不膨脹。

---

## 🛠️ 系統架構 (Architecture)

### 後端技術棧 (Backend Server)

* **核心框架**：[Gin](https://gin-gonic.com/) (Go 語言極速 Web 框架)
* **Git 底層引擎**：[go-git](https://github.com/go-git/go-git) (純 Go 實現，不依賴外部 Git 程式)
* **記憶體快取**：自研 `MemoryCache` (帶 `sync.RWMutex` 讀寫鎖)
* **線程安全**：`sync.Mutex` 全域寫入鎖，確保高併發檔案寫入安全

### 客戶端技術棧 (CLI Client - gitlocal.go)

* **自動專案判定**：抓取現行工作目錄名稱 (`filepath.Base(workDir)`)，實現原生 Git 體驗。


* **網路通訊**：Go 內建 `net/http` 與 `multipart` 表單封裝，提供極速的二進位串流傳輸。


* **參數解析**：Go 原生 `os.Args` 智慧型雙軌路由解析。


* **封裝工具**：Go 編譯器原生 `go build` (可編譯為單一跨平台執行檔)。



---

## 📂 專案結構 (Directory Structure)

```text
GitLocal/
├── main.go              # 伺服器主程式進入點 (環境與配置初始化)
├── routes.go            # 伺服器 API 路由與 Gin 控制器 (含備份、匯入與 API 分頁)
├── database.go          # Git 核心引擎 (Go 原生 GC、Revert、LFS、快取與 3-Way Merge)
├── gitlocal.go          # 遠端指令列工具 (CLI 客戶端腳本)
├── .gitignore           # 智慧忽略配置檔 (定義 CLI 與 Web 排除追蹤的垃圾檔案)
├── .gitlocal            # (使用者自建) 存放遠端伺服器 URL 的純文字設定檔
├── my_git_repos/        # (伺服器自動生成) 所有本地 Git 倉庫存放地
└── templates/           # 前端模板 (Go html/template)
    ├── index.html       # 工作區首頁 (雙欄 IDE、雙卡片建立與 ZIP 拖曳/選擇匯入)
    ├── repo.html        # 倉庫主頁 (檔案總管、非同步歷史紀錄、Releases、Web 終端機)
    ├── edit.html        # 程式碼線上編輯器 (內建三大引擎與檔案樹)
    └── commit.html      # Commit 歷史詳細差異比對 (Diff) 視窗

```

---

## 🚀 快速啟動 (Quick Start)

### 1. 啟動 GitLocal 伺服器 (Server)

請確保電腦已安裝 **Go 1.20+**，在專案目錄下執行：

```bash
# 下載依賴套件 (首次執行)
go mod tidy

# 啟動伺服器
go run main.go database.go routes.go

```

打開瀏覽器前往 `http://localhost:5001` 即可開始使用！

### 2. 設定 CLI 客戶端 (A 電腦 / 開發機)

在您的實際開發專案資料夾根目錄下，建立一個名為 `.gitlocal` 的純文字檔，填入伺服器的實體網址：

```text
http://192.168.1.100:5001

```

*(未設定時，系統將自動安全退回本機 `[http://127.0.0.1:5001](http://127.0.0.1:5001)` 作為基準。)*

---

## 💻 CLI 指令完全手冊 (CLI Reference)

以下內容完全取自 `gitlocal.go` 程式碼內建之使用說明與真實語法範例：

```text
================================================================================
 🚀 GitLocal 遠端指令列工具 (Full API Client - 雙語法完全支援版)
================================================================================
 💡 基本語法結構:
   【方法 A】預設 (自動抓當前目錄): gitlocal <指令> [參數]
   【方法 B】指定特定專案名稱:     gitlocal <專案名稱> <指令> [參數]

 🛠️ [基礎檔案與狀態操作] 
  (1) status        : 查看專案與伺服器的檔案變更狀態
      語法: gitlocal status  /  gitlocal <專案> status
      範例: gitlocal status
      範例: gitlocal GitLocal-Go status

  (2) commit        : 比對並提交變更至伺服器當前分支
      語法: gitlocal commit -m "<說明>"  /  gitlocal <專案> commit -m "<說明>"
      範例: gitlocal commit -m "修復 Bug"
      範例: gitlocal GitLocal-Go commit -m "修復 Bug"

 🌿 [分支管理與切換]
  (3) switch / checkout : 🔀 切換至指定分支 (如 master)
      語法: gitlocal switch <分支名>  /  gitlocal <專案> switch <分支名>
      範例: gitlocal switch master
      範例: gitlocal GitLocal-Go switch master

  (4) branch        : 🌿 查看所有分支 (不帶參數) 或 ➕ 建立新分支
      語法: gitlocal branch  /  gitlocal <專案> branch
      語法: gitlocal branch <新分支名> [commit_sha]  /  gitlocal <專案> branch <新分支名> [commit_sha]
      範例: gitlocal branch
      範例: gitlocal GitLocal-Go branch feature-ui

  (5) delete-branch : 🗑️ 刪除特定分支
      語法: gitlocal delete-branch <分支名>  /  gitlocal <專案> delete-branch <分支名>
      範例: gitlocal delete-branch feature-ui
      範例: gitlocal GitLocal-Go delete-branch feature-ui

  (6) merge         : 🔄 將來源分支合併至當前分支
      語法: gitlocal merge <來源分支名>  /  gitlocal <專案> merge <來源分支名>
      範例: gitlocal merge Refactoring
      範例: gitlocal GitLocal-Go merge Refactoring

 ☁️ [雲端同步與歷史下載]
  (7) push          : ☁️ 將伺服器最新進度推送到 GitHub
      語法: gitlocal push <網址> <Token>  /  gitlocal <專案> push <網址> <Token>
      範例: gitlocal push https://github.com/xxx/repo.git ghp_xxxx
      範例: gitlocal GitLocal-Go push https://github.com/xxx/repo.git ghp_xxxx

  (8) get-file      : ⬇️ 下載歷史 Commit 中的「單一特定檔案」
      語法: gitlocal get-file <sha> <路徑> <存檔>  /  gitlocal <專案> get-file <sha> <路徑> <存檔>
      範例: gitlocal get-file a1b2c3d main.go ./main_bak.go
      範例: gitlocal GitLocal-Go get-file a1b2c3d main.go ./main_bak.go

  (9) zip           : 📦 下載歷史時間點的「完整專案打包 ZIP」
      語法: gitlocal zip <sha> <存檔.zip>  /  gitlocal <專案> zip <sha> <存檔.zip>
      範例: gitlocal zip a1b2c3d release.zip
      範例: gitlocal GitLocal-Go zip a1b2c3d release.zip
================================================================================

```

---

## 📦 獨立執行檔打包 (免 Go 環境部署)

Go 語言可直接編譯為無環境依賴的單一執行檔（Executable）：

### 1. 編譯 Web 伺服器端

```bash
# Windows (背景/無主控台視窗)
go build -ldflags "-H=windowsgui" -o gitlocal-server.exe main.go database.go routes.go

# Windows (帶主控台 Log 輸出)
go build -o gitlocal-server.exe main.go database.go routes.go

```

### 2. 編譯 CLI 工具 (gitlocal)

```bash
# Windows 系統
go build -o gitlocal.exe gitlocal.go

# Linux / macOS 系統
go build -o gitlocal gitlocal.go

```

---

## 📄 授權條款 (License)

本專案採用 **[MIT 授權條款](https://opensource.org/licenses/MIT)** 進行開源授權，您可以自由使用、修改與分發。