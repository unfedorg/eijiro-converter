# eijiro-converter

英辞郎のテキストデータを、GoldenDictなどの辞書アプリケーションで利用可能なStarDict形式に変換するGo製のコマンドラインツールです。

## 主な機能

*   **柔軟なカスタマイズ**: 発音記号、例文、単語レベルなど、不要な情報をオプションで細かく除外できます。
*   **賢い参照解決**: `knew` から `know`、`doors` から `door` のように、動詞の活用形や名詞の複数形から原形の定義を自動的に参照し、統合します。
*   **高い互換性**: 標準的な `dictzip` 形式で圧縮し、GoldenDictをはじめとする多くの辞書アプリで快適に動作します。
*   **文字コード自動変換**: Shift_JIS形式の英辞郎テキストを自動でUTF-8に変換します。

## 必須要件

*   Go (1.24.2 or later)
*   `dictzip` コマンドラインツール
*   英辞郎のテキストデータファイル (例: `EIJIRO-1448.TXT`)

### `dictzip`のインストール

お使いのシステムに合わせて、`dictzip`をインストールしてください。

*   **macOS (Homebrew):**
    ```sh
    brew install dictzip
    ```
*   **Debian/Ubuntu:**
    ```sh
    sudo apt-get install dictzip
    ```

## 使い方

1.  このリポジトリをクローンします。
2.  英辞郎のテキストファイル (`EIJIRO-1448.TXT`など) をこのプロジェクトのディレクトリに配置します。
3.  ターミナルで下記のコマンドを実行します。

### 基本的な変換

```sh
go run eijiroconverter.go
```

### 情報を最小限にした辞書を作成

```sh
go run eijiroconverter.go -minimal
```

成功すると、`output_stardict` ディレクトリに `Eijiro.ifo`, `Eijiro.idx`, `Eijiro.dict.dz` の3つのファイルが生成されます。このディレクトリを、お使いの辞書アプリケーション（GoldenDictなど）の辞書フォルダにコピーしてください。

## コマンドラインオプション

| Flag | 説明 | デフォルト値 |
|:---|:---|:---:|
| `-i` | 入力する英辞郎ファイル名 | `EIJIRO-1448.TXT` |
| `-o` | 出力先ディレクトリ | `output_stardict` |
| `-b` | 辞書の名前 | `Eijiro` |
| `-strip-pdic-link` | PDICリンク(<→…>)を削除する | `false` |
| `-minimal` | 下記のすべての追加情報を除外し、最小限の定義のみを対象とする | `false` |
| `-no-examples` | 用例(■・)を除外する | `false` |
| `-no-supplement` | 補足説明(◆)を除外する | `false` |
| `-strip-ruby` | 読み仮名({…})を削除する | `false` |
| `-strip-pronunciation` | 発音記号(【発音】…)を削除する | `false` |
| `-strip-katakana` | カタカナ発音(【＠】…)を削除する | `false` |
| `-strip-forms` | 変化形(【変化】…)を削除する | `false` |
| `-strip-level` | 単語レベル(【レベル】…)を削除する | `false` |
| `-strip-syllabification` | 分節(【分節】…)を削除する | `false` |
| `-strip-other-labels` | 品詞({名})やその他のラベル({大学入試})を削除する | `false` |
| `-single-word-only` | 見出語が単一の単語からなるもののみを対象とする | `false` |

## 開発

### テストの実行

プロジェクトには、主要な変換ロジックを検証するためのテストが含まれています。テストを実行するには、`EIJIRO-1448.TXT`をプロジェクトルートに配置した上で、以下のコマンドを実行してください。

```sh
go test -v
```