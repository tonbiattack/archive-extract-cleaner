# ZipSweep — Microsoft Store 配布準備（未提出）

Issue [#60](https://github.com/tonbiattack/archive-extract-cleaner/issues/60) の検討・受け入れ記録。**Store 公開の可否は未確定**です。チェック項目は実施した証拠がある場合のみ完了にします。本人の最終確認なしに予約・申請・公開しません。ZipSweep は仮称で、Store の名前予約・商標調査は未完了です。

## 提出方式の候補と現行版との差

第一候補は **MSIX の Store 提出**です。Microsoft は MSIX をホストして再署名し、Store 管理の更新を提供します。一方、Store の MSI/EXE 提出は公開 HTTPS 上の適切なインストーラーと発行者による署名が必要で、更新はアプリ側の責任です。[公式比較](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/choose-distribution-path)。このプロジェクトの既存 GitHub Releases はインストーラーではなく単体 EXE で、自動更新・Store のパッケージ識別子・Store の署名はありません。

**まだ方式を確定しません。** 現在のファイルアクセス、WebView2、7-Zip およびごみ箱の安全性、パッケージ化後の既存データ移行を実機で確認する必要があります。これらが成立しなければ従来 Win32 インストーラー方式も比較し、申請方式を再決定します。

### パッケージ化の試行（2026-11-23）

Windows SDK `MakeAppx 10.0.26100.9457` で Wails の Windows x64 EXE、仮アイコン、**ローカル検証専用の仮 Identity / Publisher** を含む未署名 MSIX を作成・展開でき、展開した EXE の SHA-256 がビルド元と一致しました。これはパッケージ生成の可否だけを示します。署名、インストール、アプリ起動、アクセス権、更新、アンインストール、ストア認証はいずれも確認しておらず、この成果物を配布・提出してはいけません。Partner Center の正式な識別子、Store 用素材、署名された検証手順と CI は今後必要です。

- Win32/Wails の MSIX を検証する場合、`MakeAppx` と Windows SDK を使い、`packagedClassicApp` / `mediumIL` と `runFullTrust` の要件を確認します。[Microsoft の手動パッケージ化手順](https://learn.microsoft.com/en-us/windows/msix/desktop/desktop-to-uwp-manual-conversion)
- Store の **Identity Name / Publisher** は Partner Center が割り当てる値を使用します。仮の値や自己署名証明書は Store の識別子ではありません。名称変更だけで既存設定を移動しません。[申請開始・名前予約](https://learn.microsoft.com/en-us/windows/apps/publish/get-started)
- パッケージ内は読み取り専用です。AppData のリダイレクトやアンインストール後のデータの扱いはパッケージ種別・OS に依存します。既存の `%AppData%` / `%LocalAppData%` の設定、整理判定、ログ、WebView2 データとの共存・移行・削除時の保持を未確認のまま保証しません。[パッケージ化デスクトップアプリの動作](https://learn.microsoft.com/en-us/windows/msix/desktop/desktop-to-uwp-behind-the-scenes)
- Win32 フル信頼は無制限のアクセス保証ではありません。ユーザー権限、ドライブの状態、ポリシー、ネットワークやごみ箱の対応に従います。アクセス不能時は未検証・失敗として保持し、完全削除にフォールバックしないことを確認します。
- Windows 11 の WebView2 Runtime、外部 `7z.exe` と multi-drive-copy へのアクセス・起動を検証します。`.7z` / `.rar` は 7-Zip 不在時に未検証として保持します。[WebView2 配布](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/distribution)

## 審査・掲載素材

- 説明文: [STORE_LISTING.md](STORE_LISTING.md) に短い説明・長い説明・検索キーワードを用意した。
- アイコン: `docs/store/icons/` に Store が要求するサイズを置いた（`python tools/make_icon.py` で再生成できる）。自作のため第三者の素材を含まない。
- スクリーンショット: `docs/store/screenshots/` に 2 枚。`tools/make_sample_data.py` で生成した架空のサンプルデータを使い、個人名・実在のファイル名・機密情報を含まない。
- プライバシーポリシー: [PRIVACY.md](PRIVACY.md)。GitHub Pages（`https://tonbiattack.github.io/archive-extract-cleaner/PRIVACY.html`）で公開する。
- サポート先: GitHub Issues。
- 年齢区分・配布地域は本人が確認する（未実施）。

### Partner Center の識別子（確定済み）

2026-11-23 に名前予約が完了した。正式な識別子は次のとおり。

| 項目 | 値 |
| --- | --- |
| ストア ID | `9P1CF06QFZG6` |
| Identity Name | `tonbiattack.ZipSweep` |
| Publisher | `CN=2F14A66E-02DF-43E2-93E0-5C5900B38D2E` |
| PublisherDisplayName | `tonbiattack` |
| PFN | `tonbiattack.ZipSweep_e28ksxe3fc9w4` |

名前予約の有効期限は約3か月（2027年2月下旬）。それまでに初回申請を送信する。

正式な識別子でのビルドは次のとおり。生成物は `build/msix/ZipSweep.msix`（約 5.1 MB）。

```powershell
$env:MSIX_IDENTITY_NAME = "tonbiattack.ZipSweep"
$env:MSIX_PUBLISHER     = "CN=2F14A66E-02DF-43E2-93E0-5C5900B38D2E"
$env:MSIX_PUBLISHER_DISPLAY_NAME = "tonbiattack"
$env:MSIX_VERSION       = "0.1.0.0"
pwsh tools/build-msix.ps1 -Exe build/bin/archive-extract-cleaner.exe -Out build/msix
```

### マニフェストで間違えやすい点

`Properties/PublisherDisplayName` は **Partner Center の「発行者表示名」** と完全一致している必要がある。アプリ名（ZipSweep）を入れると Store のパッケージ受領検証で弾かれる。

```text
アプリ マニフェスト内の PublisherDisplayName 要素が ZipSweep です。
発行元の表示名 tonbiattack と一致しません。
```

**WACK はこの項目を照合しない。** ローカルで PASS しても Store 受領で落ちるため、CI の `Verify MSIX manifest matches Partner Center` で `Identity` と `Properties` の値を突き合わせている。

### 名称の予備調査（2026-11-23）

ZipSweep という名称について、公開情報の範囲で予備調査した。

| 調査 | 結果 |
| --- | --- |
| Microsoft Store でのアプリ名検索 | 「ZipSweep」に一致するアプリは見つからなかった |
| 一般的な Web 検索 | 「ZipSweep」に一致するソフトウェアは見つからなかった |

**これは法的な商標調査ではない。** 公式な商標データベース（USPTO / J-PlatPat など）の網羅的な調査と、弁理士等による確認は行っていない。Store の名前予約時に空きが無ければ、名称を調整する。

## 安全性・配布受け入れチェックリスト

本番データではなく、復元可能な専用テストデータとバックアップを用います。**自動テストで確認できた項目と、実機・アカウントが必要な項目を区別**します。

### 自動テストで確認済み（2026-11-23）

- [x] 破損 ZIP、内容不一致、7-Zip 未導入の `.7z` が削除候補にならないこと（`acceptance_test.go`）
- [x] スキャン後にフォルダが変更された場合、移動せずスキップすること（`acceptance_test.go`）
- [x] `UNVERIFIED` / `AMBIGUOUS` / `MISMATCH` が移動 API で拒否されること（`store_safety_test.go`）
- [x] スキャン後にアーカイブが変更された場合、移動しないこと（`store_safety_test.go`）
- [x] リンク・ジャンクション混入、上限超過、暗号化 ZIP が未検証になること（`internal/verify`）
- [x] 設定・整理状態・操作ログ・キャッシュの保存先が旧名称のまま維持されること（`branding_test.go`）
- [x] MSIX のペイロードがビルドした EXE と一致すること（CI の `Verify MSIX payload`）

### 実機・アカウントが必要（未実施）

- [ ] 旧 EXE とパッケージ版の並存・更新、設定・整理状態・操作ログ・画面設定の移行または維持を確認。アンインストールがユーザーデータへ及ぼす影響を確認。
- [ ] 外付けドライブの走査、アクセス拒否、切断、読み取り失敗で未検証を削除候補にしない。
- [ ] 確認済み項目をごみ箱へ移動し、Windows のごみ箱から復元。非対応ドライブ・権限不足では完全削除せず失敗する。
- [ ] 7-Zip 導入済みの環境で `.7z` / `.rar` を照合できること。パッケージ内から外部 `7z.exe` を起動できること。
- [ ] WebView2 Runtime 不在または更新時の動作。
- [ ] multi-drive-copy 未導入・導入済みの動作。
- [ ] store 配布のインストール・更新・アンインストールと GitHub 版の差分を記録。証明書の信頼範囲と削除手順を確認。

### CI で確認済み: 署名・インストール・WACK（2026-11-23）

`.github/workflows/msix-acceptance.yml` を手動実行し、次を確認した（run 45199074036）。

| 項目 | 結果 |
| --- | --- |
| テスト証明書の作成・信頼 | 成功 |
| signtool による署名 | 成功（`signature status: Valid`） |
| 署名者 | `CN=2F14A66E-02DF-43E2-93E0-5C5900B38D2E` |
| MSIX のインストール | **成功** |
| インストールされたパッケージ | `tonbiattack.ZipSweep_0.1.0.0_x64__e28ksxe3fc9w4`（PFN と一致） |
| 発行者 | `CN=2F14A66E-02DF-43E2-93E0-5C5900B38D2E` |
| Windows App Certification Kit | **`OVERALL_RESULT="PASS"`**（テスト 24 件すべて PASS、FAIL・WARNING なし） |
| アンインストール | 成功 |

WACK のレポートは次のとおり。

- ツール版: `10.0.26100.9457`
- `APP_TYPE="Centennial"`、`APP_NAME="tonbiattack.ZipSweep"`、`APP_VERSION="0.1.0.0"`
- 実行環境: Microsoft Windows Server 2025 Datacenter（GitHub Actions ランナー）

**注意:** これは GitHub Actions のランナー上での結果です。クリーンな Windows 11 の実機（一般利用者の環境）での確認を代替しません。また、WACK の PASS は Store 認証の通過を保証するものではありません。

### ローカル環境で確認した事実（2026-11-23）

| 項目 | 結果 |
| --- | --- |
| OS | Windows 10 Pro 2009（ビルド 26300） |
| WebView2 Runtime | 158.0.4370.33 導入済み |
| 7-Zip CLI | **未導入**（`.7z` は未検証になることを確認） |
| ドライブ | C / E / F / G すべて NTFS 固定ディスク |
| 未署名 MSIX の生成・展開 | 成功（ペイロードのハッシュ一致） |
| MSIX の登録（`Add-AppxPackage -Register`） | **失敗**（`0x80073CFF`: 開発者用ライセンスまたはサイドローディング対応が必要） |
| Windows App Certification Kit | ローカルでは**実行不可**（`appcert.exe` が管理者特権を要求） |

**ローカルは非管理者かつ Windows 10 のため、署名・インストール・WACK はここでは実行できません。** 代わりに GitHub Actions のランナー（管理者権限）で実行し、上記のとおり成功した。ただしランナーは Windows Server 2025 で、Windows 11 の実機ではない。

## 現在の実装と残作業

現行 CI は Windows 上で Go の検証、フロントエンドテスト、Wails EXE ビルド、Store 用素材の生成、**正式な識別子での MSIX ビルド**とペイロード検証を行います。GitHub Releases には EXE を配置します。

CI は正式な識別子（`tonbiattack.ZipSweep` / `CN=2F14A66E-02DF-43E2-93E0-5C5900B38D2E`）で MSIX を組み立て、ペイロードの一致を検証します。成果物は `zipsweep-msix` です。

署名・インストール・WACK は `msix-acceptance` ワークフロー（手動実行）で確認済みです。残るのは次のとおりです。

- Windows 11 実機でのパッケージ版の動作確認（旧 EXE との並存・更新、設定の引き継ぎ、外部 `7z.exe`、WebView2、外付けドライブ、ごみ箱からの復元）
- Partner Center への申請と公開（本人の最終承認）

#60 は上記が完了するまでオープンのままにします。
