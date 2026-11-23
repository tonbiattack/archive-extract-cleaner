# ZipSweep

ZIP などのアーカイブと解凍済みフォルダを内容まで照合し、**完全一致したフォルダだけ**をごみ箱へ移動できる Windows 用ツールです。

- [プライバシーポリシー](PRIVACY.md)
- [仕様書](SPEC.md)
- [Store 配布準備](STORE_READINESS.md)
- [Store 掲載文](STORE_LISTING.md)
- [GitHub リポジトリ](https://github.com/tonbiattack/archive-extract-cleaner)

## 概要

同じ親フォルダにある `name.zip` と `name/` を対応付け、アーカイブを展開せずエントリをストリーミングで SHA-256 比較します。名前が同じというだけでは判断せず、全ファイルと空ディレクトリまで一致した組だけを候補にします。

スキャンとプレビューは読み取り専用です。移動は確認した項目だけで、完全削除は行いません。

## 動作環境

- Windows 11（x64）
- WebView2 Runtime（Windows 11 は通常プリインストール済み）
- `.7z` / `.rar` の照合には [7-Zip](https://www.7-zip.org/) の CLI が必要です

## ご注意

**重要なデータは事前にバックアップしてください。** ごみ箱への移動はバックアップの代わりになりません。ごみ箱へ移した容量は、ごみ箱を空にするまでディスクの空き容量として戻りません。
