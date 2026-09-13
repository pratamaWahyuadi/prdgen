# prdgen

CLI buat generate PRD, LLD (Low-Level Design), dan GitHub issues dari satu
ide aplikasi -- lewat serangkaian agent AI yang masing-masing fokus satu
tugas kecil (tanya-jawab detail, audit keamanan, tulis PRD, desain schema,
desain API, susun coding plan, tulis issue), bukan satu AI besar yang
disuruh ngerjain semuanya sekaligus.

Kenapa dipecah gitu? Karena AI yang dikasih tugas kebanyakan sekaligus
cenderung ngarang atau lupa detail. Dengan dipecah, tiap agent cuma perlu
fokus satu hal, dan hasil satu agent jadi input buat agent berikutnya.

Contoh hasil prdgen: https://github.com/pratamaWahyuadi/prdgen

---

## Daftar Isi

- [Sebelum mulai](#sebelum-mulai)
- [Instalasi](#instalasi)
- [Panduan buat yang baru pertama kali pakai](#panduan-buat-yang-baru-pertama-kali-pakai)
- [Command reference](#command-reference)
  - [`prdgen new`](#prdgen-new---bikin-prd)
  - [`prdgen lld`](#prdgen-lld---bikin-low-level-design)
  - [`prdgen issues`](#prdgen-issues---generate-github-issues)
  - [`prdgen revise`](#prdgen-revise---perbaiki-dokumen-yang-sudah-jadi)
- [Setelah issue dibuat: cara pakai bareng AI coding agent](#setelah-issue-dibuat-cara-pakai-bareng-ai-coding-agent)
- [Semua file yang dihasilkan](#semua-file-yang-dihasilkan)
- [Cara kerja resume & checkpoint](#cara-kerja-resume--checkpoint)
- [Edge case yang sudah ditangani](#edge-case-yang-sudah-ditangani)
- [Keterbatasan yang belum ditangani](#keterbatasan-yang-belum-ditangani)
- [Custom prompt tanpa rebuild](#custom-prompt-tanpa-rebuild)
- [Testing](#testing)
- [Ganti provider LLM](#ganti-provider-llm)

---

## Sebelum mulai

**Sebelum lo buka `prdgen`, matangin dulu ide lo di luar tool ini.** Ngobrol
sama AI chat biasa (ChatGPT/Claude/dll) buat brainstorming, gambar alurnya
di Excalidraw atau kertas. `prdgen` didesain buat mengubah ide yang udah
cukup jelas jadi dokumen teknis terstruktur -- bukan buat bantu lo mikirin
ide dari nol. Semakin jelas ide awal yang lo kasih, semakin sedikit asumsi
yang harus ditebak sistem, dan semakin bagus hasilnya.

---

## Instalasi

Butuh Go 1.22+ terinstall.

### Cara 1 — `go install` (paling gampang, direkomendasikan)

```bash
go install github.com/pratamaWahyuadi/prdgen/cmd/prdgen@latest
```

Selesai. Binary `prdgen` otomatis masuk ke `$GOBIN` (`~/go/bin/` secara
default) dan bisa dipanggil dari mana saja -- asal folder itu ada di
`$PATH` lo. Kalau belum, tambahkan sekali ke shell rc (`~/.bashrc` /
`~/.zshrc`):

```bash
export PATH="$PATH:$HOME/go/bin"
```

Update ke versi terbaru: jalankan command `go install` yang sama lagi.

**Kenapa path-nya panjang dan ada `/cmd/prdgen` di belakang?** Ini bagian
yang paling sering bikin orang bingung, jadi dijelasin di sini:

- `github.com/pratamaWahyuadi/prdgen` (tanpa `/cmd/prdgen`) itu **module**
  -- cuma alamat repo, root dari project. Di situ tidak ada kode yang
  bisa dijalankan, hanya `go.mod`. Kalau lo `go install` alamat module
  saja, Go komplain:

  ```
  go: module github.com/pratamaWahyuadi/prdgen found (vX.Y.Z),
      but does not contain package github.com/pratamaWahyuadi/prdgen
  ```

- `.../cmd/prdgen` itu **package** tempat fungsi `main()` berada (file
  `cmd/prdgen/main.go` di repo ini). Yang di-install jadi binary itu
  package yang punya `main()`, bukan module-nya. Jadi aturannya:
  **module + path ke folder package main**. Nama binary yang dihasilkan
  mengikuti nama folder terakhir (`prdgen`), bukan nama module.

- `@latest` = ambil versi rilis tertinggi yang **sudah di-tag & di-push ke
  GitHub** (misal `v0.1.1`). Bukan commit terbaru apa adanya -- kalau
  belum ada tag `vX.Y.Z` di repo, `@latest` gagal dengan error
  `no matching version`. Kalau lo butuh versi spesifik, ganti
  `@latest` jadi `@v0.1.1`.

- Kalau lo jalanin `go install` dari **dalam** folder module lain,
  Go kadang komplain `requires a version when current directory is not
  in a module`. Itu normal -- tinggal pastikan pakai suffix versi
  (`@latest` atau `@vX.Y.Z`) seperti command di atas, dan tidak perlu
  `cd` ke mana-mana.

### Cara 2 — clone & build manual (untuk development lokal)

```bash
git clone https://github.com/pratamaWahyuadi/prdgen.git
cd prdgen
go build -o prdgen ./cmd/prdgen
```

Ini menghasilkan satu binary `prdgen` di folder repo. Pindahin ke folder
yang ada di `$PATH` kalau mau bisa dipanggil dari mana saja (opsional):

```bash
sudo mv prdgen /usr/local/bin/
# atau tanpa sudo, khusus user lo:
mkdir -p ~/bin && mv prdgen ~/bin/   # pastikan ~/bin ada di $PATH
```

### Setup API key

```bash
cp .env.example .env
```

Edit `.env`, isi (contoh untuk DeepSeek; lihat `.env.example` untuk
Gemini):

```
DEEPSEEK_API_KEY=sk-xxxxxxxxxxxxxxxxxxxxxxxx
DEEPSEEK_MODEL=deepseek-chat
```

Kalau lo install via `go install` (Cara 1), file `.env.example` tidak ada
di komputer lo -- bikin file `.env` sendiri di folder tempat lo
menjalankan `prdgen`, atau [export langsung di
shell](https://www.gnu.org/software/bash/manual/bash.html#Environment-Variables)
(`export DEEPSEEK_API_KEY=...`).

`prdgen` otomatis baca file `.env` -- tidak perlu `export` manual tiap buka
terminal baru. Urutan pencarian: `.env` di dalam folder project target dulu
(jadi tiap project bisa punya API key/model beda), baru fallback ke `.env`
di folder tempat lo jalanin command. Kalau lo sudah `export` manual di
shell, itu selalu menang di atas isi `.env`.

**Kenapa DeepSeek, dan kenapa boleh model reasoning yang lambat/mahal**:
tahap planning (PRD/LLD) ini krusial dan cuma dijalanin sesekali per
project -- beda dengan tahap eksekusi coding yang dijalanin berkali-kali.
Jadi wajar pakai model yang lebih pintar/lambat di sini, walau nanti pas
coding beneran lo pakai model yang lebih murah dan cepat.

---

## Panduan buat yang baru pertama kali pakai

Ini alur lengkap dari nol sampai siap coding, buat 1 project baru:

### Langkah 1 -- PRD

```bash
prdgen new ./nama-project
```

Lo akan ditanya nulis ide aplikasi (akhiri dengan baris `EOF` lalu Enter, atau
Ctrl+D). Setelah itu discovery berjalan DUA FASE terpisah dengan gate di
antaranya:

1. **Fase 1 — Discovery high-level** (8–12 pertanyaan): masalah & goal,
   scope, skala kasar, timeline, budget, bahasa+framework, database engine,
   auth, deployment, integrasi eksternal, data sensitif, sampai familiarity
   tim. Jawab sejujurnya -- kalau ada yang belum kepikiran, boleh jawab
   "belum mikir", itu juga informasi yang berguna (lebih baik daripada
   sistem menebak sendiri). Dari jawaban ini disusun **Product Brief**
   (`01c_product_brief.md`).
2. **Gate eksplisit**: "lanjut ke deep-dive teknis, atau skip dan pakai
   default eksplisit?"
   - **Lanjut (y)** — Fase 2 menggali keputusan low-level yang berisiko
     ditebak ulang per-issue oleh coding agent kalau tidak dikunci: driver
     database & strategi pool, query layer, migration tool, cache/MQ/HTTP/
     storage client, config, error handling & logging, auth implementation,
     testing, CI/CD, struktur folder (8–13 pertanyaan).
   - **Skip (n)** — semua keputusan low-level diberi default eksplisit yang
     ditulis ke `defaults.yaml`: setiap nilai dicatat dengan flag
     `[ASSUMED]` + basisnya (familiarity tim kalau kejawab, default umum
     stack kalau tidak). Asumsi jadi terkontrol & bisa diaudit, bukan
     tersembunyi di badan PRD. PRD/LLD berikutnya wajib mengutipnya dan
     coding agent dilarang bikin pool/driver/instance kedua untuk komponen
     yang sama.

Kenapa dipecah dua fase: sesi tunggal 15+ pertanyaan campur high-level &
low-level bikin user kewalahan, jawab asal-asalan di pertanyaan akhir
(padahal justru itu yang paling krusial), dan gak punya konteks jawab
low-level karena arsitektur high-level belum keformulasi.

Setelah discovery selesai, sistem otomatis:
1. Bikin **threat model** (analisis keamanan, tiap item pakai ID stabil
   T1/T2/... yang dikutip sampai ke GitHub issues) berdasarkan ide +
   jawaban lo (termasuk deep-dive kalau diikuti). Ditampilkan, lo diminta
   konfirmasi lanjut atau tidak.
2. Bikin **PRD final** yang menggabungkan semuanya: section keamanan dari
   threat model, sub-section "Koneksi & Driver Database" + "Instance
   Terbagi Lain" (driver/library persis + strategi instance tunggal), dan
   section 7.5 "Asumsi Teknis" (tabel semua keputusan low-level + status
   user-confirmed / [ASSUMED] + aturan dilarang instance kedua & wajib
   stop-and-ask kalau default gak cocok saat coding).
3. Jalanin **validasi otomatis**: AI lain mengecek apakah PRD konsisten
   sama jawaban discovery (termasuk menghitung coverage: pertanyaan mana
   yang terjawab/kelewat, dan apakah yang "belum kepikiran" muncul di Open
   Questions PRD -- bukan diisi tebakan). Setelah laporan tampil, lo
   langsung ditawari tiga pilihan di terminal: **(r)** revisi PRD
   otomatis berdasarkan laporan validasi, **(m)** revisi dengan feedback
   lo sendiri, atau **Enter** selesai. Setiap revisi otomatis diikuti
   re-validasi supaya `PRD_VALIDATION.md` selalu sinkron dengan PRD
   terbaru (maks 3 round biar gak muter).

### Langkah 2 -- LLD (Low-Level Design)

```bash
prdgen lld ./nama-project
```

Perlu PRD dari langkah 1 sudah ada. Ini menghasilkan 3 dokumen berurutan
(tiap dokumen jadi konteks buat dokumen berikutnya):
1. **Database schema** (ERD + detail tabel/kolom/index).
2. **API contracts** (spesifikasi tiap endpoint, request/response, error
   format).
3. **Coding plan** (dipecah per fase, file apa yang perlu dibuat, best
   practice apa yang perlu diikuti).

Di tiap tahap lo diminta konfirmasi lanjut. Setelah selesai, ada validasi
otomatis lagi -- kali ini mengecek apakah tech stack di coding plan beneran
sama dengan yang diputuskan di PRD (ini nyegah kasus kayak PRD bilang
"pakai Next.js" tapi coding plan-nya malah ngarang pakai bahasa lain).

### Langkah 3 (opsional) -- GitHub Issues

```bash
prdgen issues ./nama-project
```

Perlu LLD dari langkah 2 sudah ada. Ini mengubah coding plan jadi daftar
issue GitHub yang siap dipakai AI Coding Agent atau developer sebagai
to-do list. Detail lengkap di bagian command reference di bawah.

### Langkah 4 (kalau perlu) -- Revisi

Kalau ada bagian dokumen yang salah atau kurang pas, jangan edit manual
file-nya -- pakai:

```bash
prdgen revise ./nama-project prd     # atau: schema / api / plan
```

Detail di bagian command reference.

---

## Command reference

### `prdgen new` -- bikin PRD

```
prdgen new <project-dir>
```

Menjalankan tahap berurutan: **Discovery Fase 1** (high-level) → **Product
Brief** → **Gate deep-dive** (lanjut/skip) → **[Discovery Fase 2** deep-dive
teknis *atau* **defaults.yaml** kalau skip**]** → **Security Audit**
(threat model) → **PRD** (dokumen final) → **Validasi PRD** (cek
konsistensi + coverage otomatis).

Kalau folder project belum ada, otomatis dibuat. Kalau lo jalanin command
ini lagi di folder yang sama dan sebagian tahap sudah selesai sebelumnya
(misal kemarin sempat berhenti di tengah), otomatis **lanjut dari tahap
terakhir yang belum selesai** -- tidak mengulang dari nol, tidak manggil
AI lagi untuk tahap yang sudah beres. Resume menghormati gate juga: kalau
lo berhenti tepat setelah jawab fase 1, run berikutnya mulai dari Product
Brief; kalau lo skip deep-dive (defaults.yaml sudah ada), run berikutnya
langsung ke security. Lihat bagian resume di bawah.

### `prdgen lld` -- bikin Low-Level Design

```
prdgen lld <folder-project>
```

Butuh `PRD.md` sudah ada di folder itu (hasil `prdgen new`). Menjalankan 4
tahap berurutan: **Database Schema** -> **API Contracts** -> **Coding Plan**
-> **Validasi LLD**. Sama seperti `new`, otomatis resume kalau sebelumnya
berhenti di tengah.

### `prdgen issues` -- generate GitHub issues

```
prdgen issues <folder-project> [owner/repo] [--yes|-y]
```

Butuh `LLD_PLAN.md` sudah ada (hasil `prdgen lld`), dan butuh **GitHub CLI
(`gh`) sudah terinstall dan login** (`gh auth login` sekali di awal).
Argumen `owner/repo` opsional -- kalau tidak diisi, `gh` menebak repo dari
folder git tempat lo menjalankan command.

Sebelum issue dibuat, draft melewati **sanity check mekanis** (gratis,
tanpa LLM): field `phase` tiap issue harus persis sama dengan nama fase
heading di coding plan, dan issue yang menyentuh database/cache/queue
wajib menyebut file sumber koneksi/instance yang dikunci di plan
(anti-double-pool). Temuan ditampilkan sebagai warning; di mode review
manual lo bisa langsung perbaiki lewat `e`, dan di mode `--yes` ini
satu-satunya gerbang otomatis sebelum `gh issue create`.

Selain itu, kalau `ISSUES_CREATED.log` berisi judul yang tidak ada di draft
sekarang (tanda draft sudah di-regenerate/revisi setelah issue dibuat),
prdgen mengumumkannya eksplisit -- sebelumnya kasus ini diam-diam bikin
issue dobel atau issue pengganti gak pernah dibuat.

Flag `--yes` (atau `-y`) bisa ditaruh di posisi mana saja (misal
`prdgen issues ./proj --yes owner/repo` atau `prdgen issues ./proj owner/repo --yes`).
Penjelasan lengkapnya ada di bagian "Mode --yes" di bawah.

**Apa yang dilakukan command ini, langkah demi langkah:**

1. AI membaca PRD + schema + API contracts + coding plan, lalu menyusun
   draft issue. Hasilnya berupa **data terstruktur** (judul, isi, label per
   issue) -- disimpan ke `ISSUES.json`. Di titik ini belum ada satupun
   yang dikirim ke GitHub.
2. Tiap issue di draft ditampilkan **satu per satu**, lengkap dengan judul,
   fase, label, dan isi lengkapnya. Untuk tiap issue, lo pilih:
   - **Enter (kosong)** -- issue ini langsung dibuat di GitHub sekarang.
   - **`e`** -- kasih feedback bebas (misal "acceptance criteria-nya
     kurang spesifik, tambahin ini itu"), AI merevisi **issue ini saja**
     berdasarkan feedback tadi, lalu ditampilkan ulang buat direview lagi.
     Bisa diulang berkali-kali sampai lo puas, baru putuskan buat/skip.
   - **`s`** -- lewati, issue ini tidak dibuat.
   - **`q`** -- berhenti sekarang. Issue yang belum diproses dibiarkan di
     draft, bisa dilanjut lain waktu.
3. Setiap issue yang berhasil dibuat, judul dan link-nya dicatat ke
   `ISSUES_CREATED.log`. Kalau lo jalankan `prdgen issues` lagi nanti
   (misal setelah tekan `q` di tengah jalan), issue yang sudah tercatat di
   log ini otomatis dilewati -- **tidak akan dibuat dobel** di GitHub.

**Kenapa ini dianggap aman dijalankan** (poin penting kalau lo khawatir
soal AI yang bisa "eksekusi command"): AI di sini **tidak pernah**
menghasilkan atau menjalankan perintah terminal apapun. AI cuma
menghasilkan data (judul, isi, label -- sebagai field terpisah dalam
format JSON). Yang benar-benar memanggil `gh` di terminal adalah kode Go
biasa, lewat cara yang memisahkan tiap argumen (bukan menggabungkan jadi
satu baris perintah). Efeknya: apapun isi judul/body issue -- termasuk
kalau kebetulan mengandung karakter yang biasanya "berbahaya" di
terminal seperti `;`, backtick, `$()` -- akan selalu diperlakukan sebagai
teks biasa, tidak pernah bisa berubah jadi perintah lain yang tidak
diinginkan.

**Mode `--yes` (skip semua konfirmasi):**

```bash
prdgen issues ./nama-project --yes
```

Default command ini selalu menampilkan tiap issue dulu satu per satu buat
lo review sebelum dibuat (lihat langkah 2 di atas). Kalau lo pengen
biarkan semua issue dari draft (`ISSUES.json`) langsung dibuat tanpa
tanya-tanya lagi, tambahkan `--yes` (atau `-y`).

- **Cara kerja:** setiap issue langsung dibuat via `gh issue create`
  tanpa menampilkan prompt review. Alur `EnsureLabels` + `CreateIssue` +
  pencatatan ke `ISSUES_CREATED.log` tetap sama persis dengan mode
  interaktif -- yang di-skip cuma bagian tanya-jawabnya.
- **Aman dipakai kalau draft sudah lo baca/setujui duluan:** flag ini
  cuma mempercepat eksekusi `gh issue create` per issue. **Tidak
  memanggil LLM sama sekali** -- draft di `ISSUES.json` dianggap final,
  tidak ada yang di-generate ulang.
- Issue yang sudah pernah dibuat sebelumnya (tercatat di
  `ISSUES_CREATED.log`) tetap otomatis dilewati seperti biasa, walau
  dengan `--yes`.

### `prdgen revise` -- perbaiki dokumen yang sudah jadi

```
prdgen revise <folder-project> [prd|schema|api|plan]
```

Kalau target (`prd`/`schema`/`api`/`plan`) tidak disebutkan di command,
lo akan ditanya interaktif.

Dipakai kalau lo baca hasil `PRD.md`, `03_schema.md`, `04_api_contracts.md`,
atau `LLD_PLAN.md` dan ada bagian yang salah, kurang detail, atau perlu
diubah. Bedanya dengan validasi otomatis (`PRD_VALIDATION.md` /
`LLD_VALIDATION.md`): validasi cuma **melaporkan** masalah, `revise` yang
**benar-benar memperbaiki**.

Alurnya: lo tulis feedback bebas (bisa multi-baris, akhiri dengan baris
berisi `EOF` lalu Enter, atau Ctrl+D) -> AI merevisi dokumen itu secara
penuh (dengan konteks dokumen lain yang relevan supaya hasil revisi tetap
konsisten, misal merevisi schema akan tetap memperhatikan PRD) -> hasil
revisi ditampilkan lengkap -> lo diminta konfirmasi (y/n) sebelum file
aslinya benar-benar ditimpa.

**Penting**: merevisi satu dokumen bisa bikin dokumen turunannya jadi
tidak sinkron. Misal kalau lo revisi `PRD.md` setelah `03_schema.md` sudah
dibuat, schema itu tidak otomatis ikut berubah. Setelah revisi yang cukup
besar, jalankan ulang tahap berikutnya yang relevan (`prdgen lld` atau
`prdgen issues`) untuk generate ulang dokumen yang terpengaruh.

---

## Setelah issue dibuat: cara pakai bareng AI coding agent

Issue yang sudah dibuat di GitHub itu **bukan** dokumen yang berdiri
sendiri. Tiap issue cuma potongan kecil dari `PRD.md`, `03_schema.md`,
`04_api_contracts.md`, dan `LLD_PLAN.md` -- lihat bagian "Technical Notes"
di tiap issue, sering ada rujukan kayak "tabel users" atau "POST
/api/events" yang detailnya cuma ada di file-file itu, bukan di issue itu
sendiri. Kalau AI coding agent cuma dikasih issue-nya doang tanpa
dokumen pendukung, dia akan menebak sendiri detail seperti nama kolom
database, format response API, atau urutan error code -- dan kemungkinan
besar hasilnya meleset dari yang sudah dirancang.

Jadi alurnya begini:

### 1. Bawa dokumen pendukung ke repo kode

Copy 4 file inti ini (plus `defaults.yaml` kalau ada -- hasil skip
deep-dive) dari folder project ke folder `docs/` di repo GitHub tempat
issue-nya dibuat, lalu commit & push:

```bash
mkdir -p /path/ke/repo-kode/docs
cp PRD.md 03_schema.md 04_api_contracts.md LLD_PLAN.md /path/ke/repo-kode/docs/
# kalau lo skip deep-dive saat discovery (ada defaults.yaml), ikutkan:
cp defaults.yaml /path/ke/repo-kode/docs/ 2>/dev/null || true
cd /path/ke/repo-kode
git add docs/
git commit -m "docs: tambah PRD, schema, API contracts, coding plan"
git push
```
1a. Jika Menggunakan AI Web (ChatGPT Web / Claude.ai)
AI Web bisa langsung membaca file dari repo GitHub kamu atau kamu bisa meng-upload file .md dari folder docs/ lokal sebagai lampiran (attachment).

1b. Jika Menggunakan AI Agent Lokal / Editor (Zed Editor, Cursor, Windsurf, Claude Code)
Jangan pernah copy-paste isi file atau suruh AI baca via URL GitHub API (karena boros token, ada overhead metadata JSON, dan lebih lambat).

Karena folder docs/ sudah ada di komputer lokal kamu, manfaatkan fitur pemanggilan file internal editor secara instan:
Dengan begini, dokumennya ikut ada di repo yang sama dengan kode -- AI
coding agent yang jalan di repo itu (Claude Code, Cursor, dll) bisa baca
sendiri filenya, lo tidak perlu copy-paste isi dokumen manual tiap kali
mau ngerjain satu issue.

### 2. Kerjakan issue berurutan sesuai fase, jangan lompat

Issue-issue ini didesain berurutan (Fase 1 -> Fase 2 -> dst, lihat label
`phase-1`, `phase-2`, ...) karena saling bergantung -- Fase 2 butuh
project skeleton dari Fase 1, Fase 6 butuh module Auth dari Fase 3-5, dan
seterusnya. Selesaikan semua issue dalam satu fase dulu (dan pastikan
lolos acceptance criteria-nya) sebelum mulai fase berikutnya.

### 3. Buka branch dari issue

```bash
gh issue develop <nomor-issue> --checkout
```

Ini bikin branch yang otomatis ter-link ke issue tersebut, jadi kalau PR
dari branch ini di-merge, issue-nya otomatis ke-close.

### 4. Kasih AI coding agent konteks: issue + dokumen

Buka terminal di root repo kode (yang folder `docs/`-nya sudah ada), lalu
jalankan agent (Claude Code, Cursor, dll) dengan prompt yang eksplisit
nyuruh dia baca issue dan bagian dokumen yang relevan dulu, baru mulai
coding. Contoh template prompt:

```
Kerjakan GitHub issue #<nomor> di repo ini.

Langkah yang harus kamu ikuti:
1. Baca isi issue-nya dulu: `gh issue view <nomor>`
2. Baca bagian yang relevan dari dokumen pendukung di folder docs/
   sebelum mulai coding:
   - docs/PRD.md          -> konteks produk & requirement
   - docs/03_schema.md    -> skema database (ERD), jangan menyimpang
     dari nama tabel/kolom/constraint yang sudah ditentukan di sini
   - docs/04_api_contracts.md -> kontrak API (request/response/error
     code), jangan ubah format response sendiri
   - docs/LLD_PLAN.md     -> coding plan, cek fase yang sesuai issue
     ini untuk detail file yang perlu dibuat/diubah
3. Implementasikan SEMUA poin di "Acceptance Criteria" issue ini --
   jangan ada yang terlewat, jangan nambah scope di luar issue.
4. Ikuti "Technical Notes" di issue secara ketat, terutama detail
   teknis yang merujuk ke section dokumen tertentu.
5. Kalau ada bagian issue yang bertentangan dengan dokumen di docs/,
   berhenti dan tanya ke aku, jangan menebak sendiri mana yang benar.
6. Setelah selesai, jalankan test yang relevan (kalau sudah ada), lalu
   ringkas di akhir: poin acceptance criteria mana saja yang sudah
   terpenuhi dan bagaimana caranya (misal ditest di mana / dicek apa).

Jangan mulai coding sebelum langkah 1 dan 2 selesai kamu baca.
```

Sesuaikan detail command (`gh issue view`) dan nama file kalau agent yang
lo pakai tidak punya akses `gh` CLI langsung -- dalam kasus itu, paste isi
issue-nya manual di awal prompt.

### 5. Review pakai acceptance criteria sebagai checklist QA

Tiap issue punya daftar `- [ ]` di bagian Acceptance Criteria. Sebelum
approve PR dari agent, minta dia jelasin satu-satu poin itu sudah
dipenuhi gimana (atau tulis test yang membuktikannya). Jangan approve
PR yang cuma bilang "sudah selesai" tanpa rincian per poin.

### 6. Satu issue = satu PR

Jangan gabungin banyak issue dalam satu PR/commit besar. Selain lebih
gampang direview, kalau ada satu bagian yang salah, gampang di-revert
tanpa mempengaruhi issue lain yang sudah beres.

---

## Semua file yang dihasilkan

Semua tersimpan di folder project yang lo tentukan (`<folder-project>/`):

| File | Dari command | Isi |
|---|---|---|
| `00_idea.md` | `new` | Ide mentah yang lo tulis di awal |
| `01a_discovery_questions.md` | `new` | Pertanyaan discovery fase 1 (tersimpan sebelum lo jawab) |
| `01b_discovery_qa.md` | `new` | Pertanyaan fase 1 + jawaban lo |
| `01c_product_brief.md` | `new` | Product Brief (ringkasan terstruktur fase 1) |
| `01d_deep_dive_questions.md` | `new` | Pertanyaan deep-dive fase 2 (kalau lo pilih lanjut) |
| `01e_deep_dive_qa.md` | `new` | Brief + pertanyaan deep-dive + jawaban lo (kalau lanjut) |
| `defaults.yaml` | `new` | Default eksplisit semua keputusan low-level, flag `[ASSUMED]` + basis (kalau skip deep-dive) |
| `02_threat_report.md` | `new` | Threat model dari Security Auditor (ID stabil T1, T2, ...) |
| `PRD.md` | `new` | PRD final (termasuk section 7.5 Asumsi Teknis) |
| `PRD_VALIDATION.md` | `new` | Laporan cross-check PRD vs discovery + coverage jawaban + audit flag [ASSUMED] |
| `03_schema.md` | `lld` | Database schema & ERD + blok Konvensi Lintas-Tabel |
| `04_api_contracts.md` | `lld` | Spesifikasi API + kolom auth per endpoint |
| `LLD_PLAN.md` | `lld` | Step-by-step coding plan (Fase 1: koneksi SSoT + instance terbagi + konvensi error) |
| `LLD_VALIDATION.md` | `lld` | Laporan cross-check tech stack: PRD vs schema/API/plan + instance non-DB |
| `ISSUES.json` | `issues` | Draft GitHub issues (data terstruktur, bisa diedit manual) |
| `ISSUES_CREATED.log` | `issues` | Catatan issue yang sudah berhasil dibuat di GitHub |

---

## Cara kerja resume & checkpoint

Prinsip dasarnya: **tiap tahap disimpan ke file begitu selesai, dan
keputusan "lanjut dari tahap mana" dicek murni dari file mana saja yang
sudah ada** -- bukan disimpan di memori proses. Jadi:

- Kalau `prdgen` di-Ctrl+C, error karena internet putus, atau sengaja
  ditutup di tengah jalan, tidak ada progress yang hilang lebih dari 1
  tahap terakhir yang belum sempat tersimpan.
- Jalankan command yang sama lagi kapan saja (besok, minggu depan) --
  otomatis lanjut dari tahap terakhir yang belum selesai. Tidak akan
  mengulang tahap yang sudah beres, jadi tidak ada API call yang
  terbuang percuma.
- File kosong (0 byte) **tidak dianggap selesai** -- kalau suatu tahap
  gagal di tengah dan sempat menghasilkan file kosong, run berikutnya
  akan mengulang tahap itu, bukan menganggapnya sudah beres.

---

## Edge case yang sudah ditangani

Ini daftar situasi spesifik yang sudah ditangani lewat desain sistemnya,
bukan cuma "kebetulan jalan":

- **Pertanyaan discovery hilang kalau di-Ctrl+C sebelum sempat jawab
  semua.** Pertanyaan dari AI disimpan ke `01a_discovery_questions.md`
  *segera* setelah di-generate, sebelum lo diminta jawab. Kalau lo
  berhenti di tengah dan lanjut lagi nanti, pertanyaan yang sama persis
  akan ditampilkan lagi (tidak generate ulang / tidak manggil AI lagi),
  lo tinggal jawab dari awal.
- **Cancel setelah threat report selesai, sebelum PRD.** Threat report
  disimpan ke disk *sebelum* lo diminta konfirmasi "lanjut ke PRD?". Kalau
  lo jawab tidak atau berhenti di situ, lalu jalankan `prdgen new` lagi,
  sistem langsung lompat ke pembuatan PRD (skip discovery & security yang
  sudah beres), tidak mengulang dari nol.
- **AI mengembalikan respons kosong.** Kadang API mengembalikan status
  sukses tapi isinya kosong (biasanya karena model reasoning kehabisan
  token buat "mikir" sebelum sempat menjawab). Ini dideteksi dan langsung
  jadi error yang jelas, bukan diam-diam menyimpan file kosong yang nanti
  dianggap "selesai" secara keliru.
- **Model membungkus JSON dalam markdown code fence.** Untuk fitur
  `issues`, kadang AI membungkus JSON hasilnya dengan tiga-backtick json
  walau sudah diminta tidak melakukan itu. Ini otomatis dibersihkan
  sebelum di-parse.
- **Issue GitHub dibuat dobel kalau `issues` dijalankan berkali-kali.**
  Issue yang sudah berhasil dibuat dicatat ke `ISSUES_CREATED.log`. Run
  berikutnya otomatis melewati issue yang judulnya sudah ada di log itu.
- **`gh` belum terinstall atau belum login.** Dicek di awal sebelum mulai
  loop pembuatan issue, supaya gagalnya cepat dengan pesan jelas --
  bukan gagal di tengah setelah sebagian issue sudah terlanjur dibuat.
- **Tech stack di LLD tidak sinkron dengan yang diputuskan PRD.** Prompt
  di setiap tahap LLD secara eksplisit diinstruksikan mengikuti section
  Tech Stack di PRD, dan ada tahap validasi otomatis terpisah
  (`LLD_VALIDATION.md`) yang mengecek ulang hal ini setelah semua
  dokumen LLD selesai.
- **Ide awal sudah menjawab sebagian pertanyaan.** Prompt discovery
  diinstruksikan tetap menggali detail lanjutan untuk kategori yang sudah
  disinggung di ide awal, bukan menganggapnya sudah cukup dan melewati
  kategori itu.
- **Output model terpotong karena kehabisan token.** Finish reason model
  (`length`/`MAX_TOKENS`) dideteksi sebelum dokumen disimpan -- dokumen
  setengah jadi ditolak dengan error jelas, bukan tersimpan lalu meracuni
  stage berikutnya sebagai konteks. Ditambah sanity check struktural
  per dokumen (code fence gak ditutup, schema tanpa mermaid, plan tanpa
  heading fase, API contracts tanpa endpoint) yang hasilnya ditampilkan
  sebagai warning.
- **Draft issues menyimpang dari coding plan.** Validasi mekanis (tanpa
  LLM): field `phase` harus cocok dengan nama fase di plan (dibandingkan
  case-insensitive, whitespace di-collapse); issue yang menyentuh
  DB/cache/queue/storage/session wajib menyebut file sumber
  koneksi/instance yang dikunci. Ini satu-satunya gerbang di mode
  `--yes`.
- **Draft issues di-regenerate setelah sebagian issue dibuat.** Judul di
  `ISSUES_CREATED.log` yang tidak match draft diumumkan eksplisit
  (sebelumnya: issue dobel atau pengganti gak pernah dibuat, diam-diam).

---

## Keterbatasan yang belum ditangani

Supaya ekspektasinya jelas -- ini yang **belum** ada solusinya:

- **Kehilangan progress kalau di-Ctrl+C di tengah mengetik jawaban.**
  Checkpoint pertanyaan sudah aman (lihat di atas), tapi jawaban yang
  sedang lo ketik (sebelum baris `EOF` penutup) belum tersimpan ke disk
  secara live per baris. Kalau berhenti di tengah mengetik jawaban,
  jawaban yang sudah diketik hilang, harus mulai jawab dari awal lagi.
  Saran sementara: siapkan jawaban di text editor dulu, baru paste
  sekaligus ke terminal.
- **Revisi satu dokumen tidak otomatis menyinkronkan dokumen
  turunannya.** Lihat catatan di bagian `prdgen revise` di atas -- ini
  keputusan desain sadar (supaya tidak boros API call regenerate semua
  dokumen tiap kali ada revisi kecil), tapi berarti lo perlu ingat sendiri
  kapan harus regenerate ulang secara manual.

---

## Knowledge injection: teknologi yang kurang umum

Kalau project lo memakai teknologi yang base knowledge AI-nya lemah
(mis. **Zitadel**, vendor spesifik, atau internal framework), hasil
planning bisa mengarang detail yang salah persis di bagian paling fatal.
prdgen bisa dikasih referensi teknis lo sendiri lewat folder:

```
<project-dir>/knowledge/*.md   (atau .txt)
```

Cara pakai:

```bash
mkdir -p ./planning/knowledge
# taruh referensi apa pun: hasil verifikasi live, spesifikasi vendor,
# catatan gotcha, potongan dokumentasi resmi
cp ~/my-references/zitadel-v4.md ./planning/knowledge/
prdgen lld ./planning
```

Semua file `.md`/`.txt` di folder itu (urut nama) otomatis di-inject ke
**setiap** panggilan model — discovery, security audit, PRD, ERD, API
contracts, coding plan, validasi, revisi, sampai generate issues — dengan
aturan presedensi eksplisit: **isi knowledge/ menang atas pengetahuan
umum model**. Kalau pengetahuan training model bertentangan dengan
referensi lo (mis. "ROPC support ada di Zitadel" padahal live-verified
tidak ada), referensi lo yang dipakai. Detail teknis tentang teknologi itu
yang tidak ada di referensi wajib ditandai sebagai asumsi yang perlu
verifikasi, bukan dikarang.

Saat aktif, prdgen menampilkan `📚 Knowledge injection aktif: N file
referensi dari <project-dir>/knowledge/` di awal run. Folder ini tidak
memengaruhi resume — dia input read-only, bisa ditambah/diubah kapan saja
sebelum run berikutnya.

Ukuran & biaya: referensi ikut terkirim di setiap panggilan, jadi jangan
taruh dokumen 50 ribu baris — taruh bagian yang relevan saja (gotcha,
endpoint, konfigurasi kritis) atau pecah per-topik.

---

## Custom prompt tanpa rebuild

Semua system prompt ada di `internal/prompts/*.txt`, di-embed ke dalam
binary supaya tidak perlu membawa folder terpisah. Untuk eksperimen ubah
prompt tanpa compile ulang:

```bash
cp -r internal/prompts ./my-custom-prompts
# edit file .txt di ./my-custom-prompts sesuka hati
export PRDGEN_PROMPT_DIR="./my-custom-prompts"
prdgen new ./nama-project
```

---

## Testing

```bash
go test ./... -v
```

Semua tahap di-test pakai provider/executor palsu (`llm.MockProvider`,
`ghissues.MockExecutor`) -- tidak ada panggilan API sungguhan, tidak butuh
API key atau `gh` asli, jadi cepat dan bisa dijalankan di CI.

---

## Ganti provider LLM

Built-in saat ini: **DeepSeek** (default), **Gemini** (Google AI
Studio), dan **TokenHarbor** (tokenharbor.ai). Pilih lewat env
`LLM_PROVIDER=deepseek|gemini|tokenharbor` (lihat `.env.example`).
TokenHarbor dan DeepSeek sama-sama memakai protokol OpenAI-compatible,
jadi keduanya berbagi klien yang sama -- hanya beda endpoint. Untuk
provider lain (OpenAI-compatible, Claude, dll), bisa ditambah tanpa
mengubah logic pipeline sama sekali:

1. Implement interface `llm.Provider` (lihat `internal/llm/deepseek.go`
   atau `internal/llm/gemini.go` sebagai contoh -- cuma perlu 2 method:
   `Complete()` dan `Name()`).
2. Tambah case di fungsi `buildProvider` di `cmd/prdgen/main.go`.

Package `internal/pipeline` (tempat semua logic tahap-tahap ada) tidak
perlu disentuh sama sekali.
