package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	// 文字コード変換のためにパッケージを追加
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

// DictionaryEntry は一つの辞書エントリを保持する構造体
type DictionaryEntry struct {
	Headword   string
	Definition string
}

// StarDictInfo は .ifo ファイルに書き込む情報を保持する構造体
type StarDictInfo struct {
	Version     string
	BookName    string
	WordCount   uint32
	IdxFileSize uint32
	Author      string
	Description string
	Date        string
	SameTypeSeq string
}

// 正規表現をコンパイル（一度だけ行い、効率化）
var entryRegex = regexp.MustCompile(`^■([^:]*?)\s*:(.*)`)

// processDefinitionで利用する正規表現を事前にコンパイル
var (
	reRuby            = regexp.MustCompile(`｛.*?｝`)
	rePDICLink        = regexp.MustCompile(`<→.*?>`)
	rePronunciation   = regexp.MustCompile(`\s*[、,]?\s*【発音[!！]?】[^【】]*`)
	reKatakana        = regexp.MustCompile(`【＠】[^【】]*`)
	reForms           = regexp.MustCompile(`【変化】[^【】]*`)
	reLevel           = regexp.MustCompile(`【レベル】[^【】]*`)
	reFormsExtract    = regexp.MustCompile(`【変化】(.*)`)
	reFormParts       = regexp.MustCompile(`《.*?》(.*?)($|、)`)
	reSyllabification = regexp.MustCompile(`【分節】[^【】]*`)
	reVerbConjugation = regexp.MustCompile(`(?:\{.+?\})?\s*(.+?)の(過去形|過去分詞|現在分詞|三人称単数現在形)$`)
	reOtherLabels     = regexp.MustCompile(`【.*?】`) // 【大学入試】などを削除 ({名}などの品詞情報は対象外)
	reSpaces          = regexp.MustCompile(`\s{2,}`)
	reTrimChars       = regexp.MustCompile(`^[\s,、]+|[\s,、]+$`)
	reMultiComma      = regexp.MustCompile(`[、,]{2,}`)
)

// ParseOptions はパース時のオプションを保持する構造体
type ParseOptions struct {
	IncludeExamples      bool // 用例 (■・)
	IncludeSupplement    bool // 補足説明 (◆)
	StripRuby            bool // 読み仮名 ({})
	StripPDICLink        bool // PDICリンク (<→...>)
	StripPronunciation   bool // 発音記号 (【発音】)
	StripKatakana        bool // カタカナ発音 (【＠】)
	StripForms           bool // 変化形 (【変化】)
	StripLevel           bool // 単語レベル (【レベル】)
	StripSyllabification bool // 分節 (【分節】)
	StripOtherLabels     bool // その他のラベル ({名}, 【大学入試】など)を削除
	SingleWordOnly       bool // 見出語が単一の単語のみ
	StripBrackets        bool // 置き換え可能な語 ([...])
}

func main() {
	// --- コマンドライン引数の設定 ---
	inputFile := flag.String("i", "EIJIRO-1448.TXT", "入力する英辞郎ファイル名 (例: EIJIRO-1448.TXT)")
	outputDir := flag.String("o", "output_stardict", "出力先ディレクトリ")
	bookName := flag.String("b", "Eijiro", "辞書の名前")

	// --- パースオプションのフラグ定義 ---
	noExamples := flag.Bool("no-examples", false, "用例(■・)を除外する")
	noSupplement := flag.Bool("no-supplement", false, "補足説明(◆)を除外する")
	stripRuby := flag.Bool("strip-ruby", false, "読み仮名({…})を削除する")
	stripPDICLink := flag.Bool("strip-pdic-link", false, "PDICリンク(<→…>)を削除する")
	stripPronunciation := flag.Bool("strip-pronunciation", false, "発音記号(【発音】…)を削除する")
	stripKatakana := flag.Bool("strip-katakana", false, "カタカナ発音(【＠】…)を削除する")
	stripForms := flag.Bool("strip-forms", false, "変化形(【変化】…)を削除する")
	stripLevel := flag.Bool("strip-level", false, "単語レベル(【レベル】…)を削除する")
	stripSyllabification := flag.Bool("strip-syllabification", false, "分節(【分節】…)を削除する")
	stripOtherLabels := flag.Bool("strip-other-labels", false, "品詞({名})やその他のラベル({大学入試})を削除する")
	singleWordOnly := flag.Bool("single-word-only", false, "見出語が単一の単語からなるもののみを対象とする")
	minimal := flag.Bool("minimal", false, "すべての追加情報を除外し、最小限の定義のみを対象とする")

	flag.Parse()

	isMinimal := *minimal

	// --- パースオプションの設定 ---
	opts := ParseOptions{
		// isMinimalがtrueの場合、個別の指定に関わらず除外/削除する
		IncludeExamples:      !*noExamples && !isMinimal,
		IncludeSupplement:    !*noSupplement && !isMinimal,
		StripRuby:            *stripRuby || isMinimal,
		StripPDICLink:        *stripPDICLink, // minimalオプションの影響を受けないように変更
		StripPronunciation:   *stripPronunciation || isMinimal,
		StripKatakana:        *stripKatakana || isMinimal,
		StripForms:           *stripForms || isMinimal,
		StripLevel:           *stripLevel || isMinimal,
		StripSyllabification: *stripSyllabification || isMinimal,
		StripOtherLabels:     *stripOtherLabels || isMinimal,
		// singleWordOnlyは情報の「内容」ではなく「対象」のフィルタリングなので、minimalの対象外とする
		SingleWordOnly: *singleWordOnly,
	}

	log.Println("変換処理を開始します...")

	// 出力ディレクトリを作成
	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		log.Fatalf("出力ディレクトリの作成に失敗しました: %v", err)
	}

	// 1. 英辞郎ファイルをパース（文字コード変換もここで行う）
	entries, err := parseEijiro(*inputFile, opts)
	if err != nil {
		log.Fatalf("英辞郎ファイルのパースに失敗しました: %v", err)
	}
	log.Printf("%d件のエントリを読み込みました。", len(entries))

	// 2. 変化形の参照を解決し、定義をマージする
	finalEntries := resolveAndMergeEntries(entries)

	// 3. StarDict ファイルを生成
	err = writeStarDictFiles(*outputDir, *bookName, finalEntries)
	if err != nil {
		log.Fatalf("StarDictファイルの書き込みに失敗しました: %v", err)
	}

	log.Printf("処理が完了しました。出力先: %s", *outputDir)
}

// resolveAndMergeEntries はパースされたエントリを受け取り、変化形のリンクを解決して定義をマージする
func resolveAndMergeEntries(entries []DictionaryEntry) []DictionaryEntry {
	log.Println("変化形の参照を解決しています...")

	// 1. 全ての定義をマップに集約する（キーは小文字に統一）
	mergedEntries := make(map[string]string)
	for _, entry := range entries {
		key := strings.ToLower(entry.Headword)
		isLinkEntry := strings.Contains(entry.Definition, "@@@LINK=")

		if existingDef, exists := mergedEntries[key]; exists {
			// 既にエントリが存在する場合
			if isLinkEntry && !strings.Contains(existingDef, "@@@LINK=") {
				// 既存の定義に、新しいリンク情報を追記する
				mergedEntries[key] = existingDef + "\n" + entry.Definition
			}
		} else {
			// 新しいエントリとして追加
			mergedEntries[key] = entry.Definition
		}
	}

	// 2. リンクを解決し、定義をマージする
	for key, def := range mergedEntries {
		if strings.Contains(def, "@@@LINK=") {
			// リンク情報（例: "@@@LINK=drive"）を抽出し、元の定義から削除する
			reLink := regexp.MustCompile(`\n?@@@LINK=(.+)`)
			linkMatch := reLink.FindStringSubmatch(def)
			originalDef := reLink.ReplaceAllString(def, "")
			linkTarget := linkMatch[1]

			if baseDef, ok := mergedEntries[linkTarget]; ok {
				mergedEntries[key] = originalDef + "\n" + "---" + "\n" + baseDef
			}
		}
	}

	// 3. マップから最終的なエントリリストを再生成
	finalEntries := make([]DictionaryEntry, 0, len(mergedEntries))
	for headword, definition := range mergedEntries {
		finalEntries = append(finalEntries, DictionaryEntry{Headword: headword, Definition: definition})
	}
	return finalEntries
}

// parseEijiro は英辞郎形式のテキストファイルを解析する
// Shift_JISからUTF-8への変換機能を含む
func parseEijiro(filePath string, opts ParseOptions) ([]DictionaryEntry, error) {
	// ループの外で正規表現をコンパイルする
	posRegex := regexp.MustCompile(`^(.*?)\s*(\{.*?\})$`)

	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Shift_JISからUTF-8へのデコーダーを作成
	decoder := japanese.ShiftJIS.NewDecoder()
	// ファイルリーダーをデコーダーでラップ
	reader := transform.NewReader(file, decoder)

	var entries []DictionaryEntry
	var synonymEntries []DictionaryEntry // 変化形から原形へのリンクを保持
	scanner := bufio.NewScanner(reader)  // デコードされたリーダーをスキャンする
	var currentEntry *DictionaryEntry

	for scanner.Scan() {
		line := scanner.Text() // ここで得られるlineはUTF-8に変換済み

		matches := entryRegex.FindStringSubmatch(line)
		if matches != nil {
			// 新しいエントリの開始行 (■)
			rawHeadword := strings.TrimSpace(matches[1])
			rawDefinition := strings.TrimSpace(matches[2])

			// 【変化】タグから同義語（変化形）を抽出する
			if formsMatch := reFormsExtract.FindStringSubmatch(rawDefinition); len(formsMatch) > 1 {
				formsStr := formsMatch[1]
				// 変化形の各部分をパースする (例: 《複》doors)
				formParts := reFormParts.FindAllStringSubmatch(formsStr, -1)
				for _, part := range formParts {
					if len(part) > 1 {
						// リンク先の見出し語から品詞情報({名}など)を取り除く
						linkTarget := rawHeadword
						if posMatches := posRegex.FindStringSubmatch(rawHeadword); posMatches != nil {
							linkTarget = posMatches[1]
						}
						// `|` で区切られた複数の変化形に対応する (例: expects | expecting | expected)
						formWordsStr := strings.TrimSpace(part[1])
						formWords := strings.Split(formWordsStr, "|")

						for _, formWord := range formWords {
							trimmedFormWord := strings.TrimSpace(formWord)
							if trimmedFormWord != "" {
								synonymEntries = append(synonymEntries, DictionaryEntry{
									Headword:   trimmedFormWord,
									Definition: "@@@LINK=" + linkTarget, // StarDictのリンク形式
								})
							}
						}
					}
				}
			}

			// 同一行に定義と用例(■・)が含まれる場合、分割する
			var definition string
			var example string
			if parts := strings.SplitN(rawDefinition, "■・", 2); len(parts) > 1 {
				definition = parts[0]
				example = "■・" + parts[1]
			} else {
				definition = rawDefinition
			}

			// 見出し語から品詞情報({名}など)を分離する
			var pos string // 品詞情報
			var headword string
			if posMatches := posRegex.FindStringSubmatch(rawHeadword); posMatches != nil {
				headword = posMatches[1]
				pos = posMatches[2]
			}

			// 動詞の活用形から原形へのリンクを生成する (例: "knowの過去形" -> "@@@LINK=know")
			// この処理は品詞情報が追加された後に行う
			tempDefWithPos := pos + " " + definition
			if verbMatch := reVerbConjugation.FindStringSubmatch(tempDefWithPos); len(verbMatch) > 1 {
				baseVerb := verbMatch[1] // (know)
				definition = tempDefWithPos + "\n@@@LINK=" + baseVerb
			} else {
				// リンクに変換しない場合は、品詞情報を先頭につける
				definition = tempDefWithPos
			}

			if headword == "" {
				headword = rawHeadword
			}

			// 直前のエントリと同じ見出し語の場合、定義を追記する
			if currentEntry != nil && currentEntry.Headword == headword {
				processedDef := processDefinition(definition, opts)
				if opts.IncludeExamples && example != "" {
					// "■・" を取り除いてから追加
					processedDef += "\n" + "■" + strings.TrimPrefix(example, "■・")
				}
				if processedDef != "" {
					currentEntry.Definition += "\n" + processedDef
				}
				continue // 次の行へ
			}

			// 新しい見出し語に移るので、その前に直前のエントリをリストに追加
			if currentEntry != nil {
				entries = append(entries, *currentEntry)
			}

			// --single-word-only オプションが有効な場合、スペースを含む見出語をスキップ
			if opts.SingleWordOnly && strings.Contains(headword, " ") {
				currentEntry = nil // 現在のエントリをリセットして、後続行が処理されないようにする
				continue
			}

			// オプションに基づいて定義を加工
			definition = processDefinition(definition, opts)

			// 用例を追加する（オプションが有効な場合）
			if opts.IncludeExamples && example != "" {
				definition += "\n" + "■" + strings.TrimPrefix(example, "■・")
			}

			currentEntry = &DictionaryEntry{
				Headword:   headword,
				Definition: definition,
			}
		} else if currentEntry != nil {
			// 用例 (■・)
			if strings.HasPrefix(line, "■・") {
				if opts.IncludeExamples {
					// "■・" を取り除いて追加
					exampleLine := strings.TrimPrefix(line, "■・")
					currentEntry.Definition += "\n" + "■" + exampleLine
				}
			} else if strings.HasPrefix(line, "◆") {
				// 補足説明 (◆)
				if opts.IncludeSupplement {
					currentEntry.Definition += "\n" + line
				}
			}
		}
		// 上記以外の行（見出しにぶら下がらない行）は無視する
	}

	// 最後の見出しを追加
	if currentEntry != nil {
		entries = append(entries, *currentEntry)
	}

	// 最後に同義語エントリを追加
	entries = append(entries, synonymEntries...)

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

// processDefinition はオプションに基づいて定義文字列を加工する
func processDefinition(def string, opts ParseOptions) string {
	// 事前にコンパイルされた正規表現を使って不要な部分を削除
	if opts.StripRuby {
		def = reRuby.ReplaceAllString(def, "")
	}
	if opts.StripPDICLink {
		def = rePDICLink.ReplaceAllString(def, "")
	}
	if opts.StripPronunciation {
		def = rePronunciation.ReplaceAllString(def, "")
	}
	if opts.StripKatakana {
		def = reKatakana.ReplaceAllString(def, "")
	}
	// 【変化】タグは同義語生成に使われるため、定義からは常に削除する
	def = reForms.ReplaceAllString(def, "")
	if opts.StripLevel {
		def = reLevel.ReplaceAllString(def, "")
	}
	if opts.StripSyllabification {
		def = reSyllabification.ReplaceAllString(def, "")
	}
	if opts.StripOtherLabels {
		def = reOtherLabels.ReplaceAllString(def, "")
	}

	// 不要なスペースや区切り文字を整理
	// 1. 連続する空白を1つにまとめる
	def = reSpaces.ReplaceAllString(def, " ")
	// 2. 連続する区切り文字（コンマや読点）を1つにまとめる
	def = reMultiComma.ReplaceAllString(def, "、")
	// 3. 先頭と末尾の不要な区切り文字や空白を削除する
	def = reTrimChars.ReplaceAllString(def, "")

	// headword: definition の形式で、definitionが空になった場合
	def = strings.TrimSpace(def)
	return def
}

// writeStarDictFiles はパースしたエントリからStarDictファイルを書き出す
func writeStarDictFiles(dir, bookName string, entries []DictionaryEntry) error {
	// ファイルパスを定義
	ifoPath := filepath.Join(dir, bookName+".ifo")
	idxPath := filepath.Join(dir, bookName+".idx")
	// 一時的に非圧縮の.dictファイルを作成する
	dictPath := filepath.Join(dir, bookName+".dict")

	var idxBuf bytes.Buffer
	var dictBuf bytes.Buffer

	for _, entry := range entries {
		definitionBytes := []byte(entry.Definition)

		// --- .idx ファイルのデータを準備 ---
		idxBuf.WriteString(entry.Headword)
		idxBuf.WriteByte(0)

		// .dictファイル内でのオフセットを記録
		offset := uint32(dictBuf.Len())
		binary.Write(&idxBuf, binary.BigEndian, offset)

		// 定義データのサイズを記録
		binary.Write(&idxBuf, binary.BigEndian, uint32(len(definitionBytes)))

		// .dictファイルの内容をバッファに書き込む
		dictBuf.Write(definitionBytes)
	}

	// --- ファイル書き出し ---

	// 1. 非圧縮の.dictファイルを書き出す
	if err := os.WriteFile(dictPath, dictBuf.Bytes(), 0644); err != nil {
		return fmt.Errorf(".dict ファイルの書き込みに失敗: %w", err)
	}

	// 2. dictzipコマンドを実行して.dictを.dict.dzに圧縮する
	// dictzipは成功すると元のファイルを削除する
	cmd := exec.Command("dictzip", dictPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		// dictzipコマンドのパスが見つからない、などのエラーメッセージを出力する
		return fmt.Errorf("dictzipの実行に失敗: %w\n%s", err, string(output))
	}

	// .idx ファイルを書き込み
	if err := os.WriteFile(idxPath, idxBuf.Bytes(), 0644); err != nil {
		return fmt.Errorf(".idx ファイルの書き込みに失敗: %w", err)
	}

	// .ifo ファイルを書き込み
	ifo := StarDictInfo{
		Version:     "2.4.2",
		BookName:    bookName,
		WordCount:   uint32(len(entries)),
		IdxFileSize: uint32(idxBuf.Len()),
		SameTypeSeq: "g", // 'g' はdictzip圧縮されたUTF-8テキストを意味する
		Author:      "Converted with Go",
		Description: "A comprehensive Japanese-English dictionary based on Eijiro data, converted with eijiro-converter.",
	}
	return writeIfoFile(ifoPath, ifo)
}

// writeIfoFile は .ifo ファイルを生成する
func writeIfoFile(path string, info StarDictInfo) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	fmt.Fprintln(writer, "StarDict's dict ifo file")
	fmt.Fprintf(writer, "version=%s\n", info.Version)
	fmt.Fprintf(writer, "bookname=%s\n", info.BookName)
	fmt.Fprintf(writer, "wordcount=%d\n", info.WordCount)
	fmt.Fprintf(writer, "idxfilesize=%d\n", info.IdxFileSize)
	if info.Author != "" {
		fmt.Fprintf(writer, "author=%s\n", info.Author)
	}
	if info.Description != "" {
		fmt.Fprintf(writer, "description=%s\n", info.Description)
	}
	if info.Date != "" {
		fmt.Fprintf(writer, "date=%s\n", info.Date)
	}
	if info.SameTypeSeq != "" {
		fmt.Fprintf(writer, "sametypesequence=%s\n", info.SameTypeSeq)
	}

	return writer.Flush()
}
