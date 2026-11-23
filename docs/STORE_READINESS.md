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

## 審査・掲載素材（未完成）

- 説明文草案: **ZipSweep は Windows 向けに ZIP などのアーカイブと解凍済みフォルダを照合し、確認後にごみ箱へ整理するツールです。** 重要なデータの事前バックアップを推奨します。7z / RAR の照合には別途 7-Zip CLI が必要です。
- アプリの既存アイコンを基に Store が要求する各サイズの素材を作成し、著作権・小サイズ表示を確認する（未実施）。
- 実機スクリーンショットは個人名・ファイル名・機密情報を含まない専用サンプルデータで取得する（未実施）。
- サポート先、公開プライバシーポリシー URL、外部ツールと任意の AI 機能のデータ取り扱い、年齢区分、配布地域を本人が確認する（未実施）。[プライバシー説明の草案](PRIVACY_DRAFT.md) は公開済みポリシーではない。実際に存在しない URL や未確定の文言を掲載しない。

## 安全性・配布受け入れチェックリスト

本番データではなく、復元可能な専用テストデータとバックアップを用います。以下は **全て未実施** です。

- [ ] 署名済みテスト用 MSIX をクリーンな Windows 11 x64 にインストール・起動。署名者とパッケージ ID を記録。
- [ ] 旧 EXE とパッケージ版の並存・更新、設定・整理状態・操作ログ・画面設定の移行または維持を確認。アンインストールがユーザーデータへ及ぼす影響を確認。
- [ ] ローカル HDD（大容量データを含む）・外付けドライブの走査、アクセス拒否、切断、読み取り失敗で未検証を削除候補にしない。
- [ ] 破損 ZIP、途中失敗、リンク・ジャンクション、曖昧な同名フォルダ、検証中またはスキャン後のアーカイブ更新で安全側に停止する。
- [ ] 確認済み項目をごみ箱へ移動し、Windows のごみ箱から復元。非対応ドライブ・権限不足では完全削除せず失敗する。
- [ ] 7-Zip 未導入・導入済み、WebView2 Runtime 不在または更新時、および multi-drive-copy 未導入・導入済みの動作を確認。
- [ ] Store 配布のインストール・更新・アンインストールと GitHub 版の差分を記録。証明書の信頼範囲と削除手順を確認。
- [ ] 安全回帰テストに加え、Windows App Certification Kit と実機受け入れを実行し、結果・OS ビルド・対象ドライブ・日時を記録。
- [ ] Store 用成果物の生成、検証、署名・配布と GitHub Releases のバージョン整合を CI で実現。偽の識別子を使用した成果物を提出可能と見なさない。
- [ ] Partner Center の名前予約、発行者・プライバシー・サポート情報を確認し、本人が申請・公開を最終承認。

## 現在の実装と残作業

現行 CI は Windows 上で Go の検証、フロントエンドテストおよび Wails EXE ビルドを行い、GitHub Releases には EXE を配置します。候補状態の確認には `TestMoveRejectsUnverifiedAndAmbiguousVerdicts` と `TestMoveRejectsArchiveChangedAfterScan` を追加しました。既存の破損、リンク、同名候補や部分失敗に関するテストもありますが、実機受け入れを代替しません。Store 用パッケージのビルド・署名・配布、アップデートおよびパッケージ版の実機受け入れはまだ行っていません。#60 は上記完了までオープンのままにします。
