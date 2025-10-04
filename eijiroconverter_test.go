package main

import (
	"log"
	"os"
	"strings"
	"testing"
)

// TestEijiroConversionWithRealData は、実際の英辞郎データを使って変換フロー全体をテストします。
func TestEijiroConversionWithRealData(t *testing.T) {
	// --- テストのセットアップ ---
	// 実際の英辞郎ファイルのパスを指定
	eijiroPath := "EIJIRO-1448.TXT"

	// 英辞郎ファイルが存在しない場合はテストをスキップ
	if _, err := os.Stat(eijiroPath); os.IsNotExist(err) {
		t.Skipf("テストスキップ: 英辞郎ファイルが見つかりません (%s)", eijiroPath)
	}

	// minimal=true相当のオプションでテストする
	opts := ParseOptions{
		IncludeExamples:      false,
		IncludeSupplement:    false,
		StripRuby:            true,
		StripPDICLink:        false, // minimalでもPDICリンクは除外しない
		StripPronunciation:   true,
		StripKatakana:        true,
		StripForms:           true,
		StripLevel:           true,
		StripSyllabification: true,
		StripOtherLabels:     true,
	}

	// 1. ファイルをパース
	log.Println("テスト: 実際の英辞郎ファイルをパースしています...")
	entries, err := parseEijiro(eijiroPath, opts)
	if err != nil {
		t.Fatalf("parseEijiroでエラーが発生しました: %v", err)
	}

	// 2. 参照を解決し、定義をマージ
	finalEntries := resolveAndMergeEntries(entries)

	// 3. 結果を検証するためのマップを作成
	resultMap := make(map[string]string)
	for _, entry := range finalEntries {
		resultMap[entry.Headword] = entry.Definition
	}

	log.Println("テスト: パースとマージが完了。個別のケースを検証します...")

	// テストケースを定義
	testCases := []struct {
		name           string
		targetHeadword string
		expectedParts  []string // この単語の定義に含まれていてほしい部分文字列
		unexpectedPart string   // この単語の定義に含まれていてほしくない部分文字列
	}{
		{
			name:           "knewの定義にknowの定義が含まれる",
			targetHeadword: "knew",
			expectedParts:  []string{"{動} knowの過去形", "---", "知っている"},
		},
		{
			name:           "doorsの定義にDoors(固有名詞)とdoor(原形)の定義が含まれる",
			targetHeadword: "doors",
			expectedParts:  []string{"{バンド名}", "ドアーズ", "---", "扉"},
		},
		{
			name:           "発音記号(全角感嘆符)が正しく除去される",
			targetHeadword: "know",
			expectedParts:  []string{"知っている"},
			unexpectedPart: "no'u",
		},
		{
			name:           "同一行の例文が正しく除外される",
			targetHeadword: "zip",
			expectedParts:  []string{"元気よくやる"},
			unexpectedPart: "I've got a date",
		},
		{
			name:           "分節が正しく除外される",
			targetHeadword: "tactical",
			expectedParts:  []string{"戦術的な"},
			unexpectedPart: "tac・ti・cal",
		},
		{
			name:           "expectingの定義にexpectの定義が含まれる",
			targetHeadword: "expecting",
			expectedParts:  []string{"妊娠している", "予期する"},
		},
		{
			name:           "droveの定義にdriveの定義が含まれる",
			targetHeadword: "drove",
			expectedParts:  []string{"driveの過去形", "動物の群れ", "---", "運転する"},
			unexpectedPart: "@@@LINK=drive",
		},
		{
			name:           "PDICリンクがminimalでも除外されない",
			targetHeadword: "bunk",
			expectedParts:  []string{"<→bunkum>"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			foundDef, ok := resultMap[tc.targetHeadword]
			if !ok {
				t.Fatalf("ターゲットの単語 '%s' が見つかりませんでした。", tc.targetHeadword)
			}

			// 期待される部分文字列がすべて含まれているかチェック
			for _, part := range tc.expectedParts {
				if !strings.Contains(foundDef, part) {
					t.Errorf("単語 '%s' の定義に期待される部分文字列 '%s' が含まれていません。\n---\n実際の定義:\n%s\n---", tc.targetHeadword, part, foundDef)
				}
			}

			// 期待されない部分文字列が含まれていないかチェック
			if tc.unexpectedPart != "" && strings.Contains(foundDef, tc.unexpectedPart) {
				t.Errorf("単語 '%s' の定義に期待されない部分文字列 '%s' が含まれています。\n---\n実際の定義:\n%s\n---", tc.targetHeadword, tc.unexpectedPart, foundDef)
			}
		})
	}
}
