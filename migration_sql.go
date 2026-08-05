package main

import (
	"fmt"
	"strings"
)

// migrationStatementKeywordLimit is the number of leading words needed to
// distinguish every forbidden PostgreSQL transaction-control prefix.
const migrationStatementKeywordLimit = 2

// validateMigrationSQLSafety rejects commands that could take transaction
// ownership away from the migration runner.
//
// Each migration file is executed inside a transaction opened by Go. Allowing
// embedded SQL to commit, roll back, prepare, or otherwise manipulate that
// transaction could separate a schema change from its ledger record. This
// lightweight lexical scan reads only the first keywords of each SQL statement
// and ignores comments and quoted content; PostgreSQL still performs the full
// syntax validation when the migration runs.
func validateMigrationSQLSafety(sqlText string) error {
	statementKeywords := make(
		[]string,
		0,
		migrationStatementKeywordLimit,
	)
	// At least one unquoted SQL word must exist. This prevents a comments-only or
	// semicolon-only file from being recorded as an applied schema version.
	hasExecutableWord := false

	for index := 0; index < len(sqlText); {
		switch {
		case strings.HasPrefix(sqlText[index:], "--"):
			index = skipSQLLineComment(sqlText, index)
		case strings.HasPrefix(sqlText[index:], "/*"):
			index = skipSQLBlockComment(sqlText, index)
		case sqlText[index] == '\'':
			index = skipSQLSingleQuotedString(
				sqlText,
				index,
				isPostgresEscapeStringPrefix(sqlText, index),
			)
		case sqlText[index] == '"':
			index = skipSQLDoubleQuotedIdentifier(sqlText, index)
		case sqlText[index] == '$':
			delimiter, valid := postgresDollarQuoteDelimiter(
				sqlText,
				index,
			)
			if !valid {
				index++
				continue
			}

			index = skipSQLDollarQuotedString(
				sqlText,
				index,
				delimiter,
			)
		case sqlText[index] == ';':
			if err := validateMigrationStatementKeywords(
				statementKeywords,
			); err != nil {
				return err
			}
			statementKeywords = statementKeywords[:0]
			index++
		case isSQLIdentifierStart(sqlText[index]):
			hasExecutableWord = true
			wordEnd := index + 1
			for wordEnd < len(sqlText) &&
				isSQLIdentifierContinuation(sqlText[wordEnd]) {
				wordEnd++
			}

			if len(statementKeywords) < migrationStatementKeywordLimit {
				statementKeywords = append(
					statementKeywords,
					strings.ToLower(sqlText[index:wordEnd]),
				)
			}
			index = wordEnd
		default:
			index++
		}
	}
	if !hasExecutableWord {
		return fmt.Errorf(
			"migration contains no executable SQL outside comments and separators",
		)
	}

	return validateMigrationStatementKeywords(statementKeywords)
}

// validateMigrationStatementKeywords checks one statement's leading keywords
// against PostgreSQL transaction-control forms that the Go runner must own.
func validateMigrationStatementKeywords(keywords []string) error {
	if len(keywords) == 0 {
		return nil
	}

	forbidden := false
	switch keywords[0] {
	case "abort", "begin", "commit", "end", "rollback", "savepoint":
		forbidden = true
	case "prepare", "release", "set", "start":
		if len(keywords) == migrationStatementKeywordLimit {
			forbidden = (keywords[0] == "prepare" && keywords[1] == "transaction") ||
				(keywords[0] == "release" && keywords[1] == "savepoint") ||
				(keywords[0] == "set" && keywords[1] == "transaction") ||
				(keywords[0] == "start" && keywords[1] == "transaction")
		}
	}

	if !forbidden {
		return nil
	}

	return fmt.Errorf(
		"explicit transaction control %q is forbidden; the migration runner owns transaction boundaries",
		strings.ToUpper(strings.Join(keywords, " ")),
	)
}

// skipSQLLineComment returns the first byte after a PostgreSQL `--` comment or
// the end of the SQL string when the comment consumes the final line.
func skipSQLLineComment(sqlText string, start int) int {
	end := strings.IndexByte(sqlText[start+2:], '\n')
	if end < 0 {
		return len(sqlText)
	}

	return start + 2 + end + 1
}

// skipSQLBlockComment skips PostgreSQL block comments, including their
// supported nested form, so semicolons and keywords inside comments are inert.
func skipSQLBlockComment(sqlText string, start int) int {
	depth := 1
	for index := start + 2; index < len(sqlText); {
		switch {
		case strings.HasPrefix(sqlText[index:], "/*"):
			depth++
			index += 2
		case strings.HasPrefix(sqlText[index:], "*/"):
			depth--
			index += 2
			if depth == 0 {
				return index
			}
		default:
			index++
		}
	}

	return len(sqlText)
}

// skipSQLSingleQuotedString ignores ordinary and E-prefixed PostgreSQL string
// literals while honoring doubled quote characters in both forms.
func skipSQLSingleQuotedString(
	sqlText string,
	start int,
	escapeBackslashes bool,
) int {
	for index := start + 1; index < len(sqlText); index++ {
		if escapeBackslashes && sqlText[index] == '\\' {
			index++
			continue
		}
		if sqlText[index] != '\'' {
			continue
		}
		if index+1 < len(sqlText) && sqlText[index+1] == '\'' {
			index++
			continue
		}

		return index + 1
	}

	return len(sqlText)
}

// isPostgresEscapeStringPrefix reports whether a quote is immediately prefixed
// by the standalone E marker that enables PostgreSQL backslash escapes.
func isPostgresEscapeStringPrefix(sqlText string, quoteIndex int) bool {
	if quoteIndex == 0 ||
		(sqlText[quoteIndex-1] != 'e' && sqlText[quoteIndex-1] != 'E') {
		return false
	}
	if quoteIndex == 1 {
		return true
	}

	return !isSQLIdentifierContinuation(sqlText[quoteIndex-2])
}

// skipSQLDoubleQuotedIdentifier ignores PostgreSQL identifiers whose contents
// may legally resemble transaction keywords or contain semicolons.
func skipSQLDoubleQuotedIdentifier(sqlText string, start int) int {
	for index := start + 1; index < len(sqlText); index++ {
		if sqlText[index] != '"' {
			continue
		}
		if index+1 < len(sqlText) && sqlText[index+1] == '"' {
			index++
			continue
		}

		return index + 1
	}

	return len(sqlText)
}

// postgresDollarQuoteDelimiter reads a valid `$$` or `$tag$` opener without
// mistaking positional parameters such as `$1` for quoted SQL bodies.
func postgresDollarQuoteDelimiter(
	sqlText string,
	start int,
) (string, bool) {
	// PostgreSQL permits dollar signs inside unquoted identifiers. A quote opener
	// must therefore have a token boundary on its left; otherwise text such as
	// `table$tag$` is one identifier, not the start of a dollar-quoted body.
	if start > 0 && isPostgresIdentifierContinuation(sqlText[start-1]) {
		return "", false
	}
	if start+1 >= len(sqlText) {
		return "", false
	}
	if sqlText[start+1] == '$' {
		return "$$", true
	}
	if !isSQLIdentifierStart(sqlText[start+1]) {
		return "", false
	}

	end := start + 2
	for end < len(sqlText) && isSQLIdentifierContinuation(sqlText[end]) {
		end++
	}
	if end >= len(sqlText) || sqlText[end] != '$' {
		return "", false
	}

	return sqlText[start : end+1], true
}

// skipSQLDollarQuotedString skips a complete PostgreSQL dollar-quoted body;
// an unterminated body extends to EOF and will later fail PostgreSQL parsing.
func skipSQLDollarQuotedString(
	sqlText string,
	start int,
	delimiter string,
) int {
	bodyStart := start + len(delimiter)
	closingOffset := strings.Index(sqlText[bodyStart:], delimiter)
	if closingOffset < 0 {
		return len(sqlText)
	}

	return bodyStart + closingOffset + len(delimiter)
}

// isSQLIdentifierStart reports the ASCII characters that can begin the SQL
// keywords relevant to transaction-control validation.
func isSQLIdentifierStart(character byte) bool {
	return character == '_' ||
		character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z'
}

// isSQLIdentifierContinuation extends an ASCII SQL word with letters, digits,
// or underscores. Non-ASCII identifiers remain harmless opaque separators.
func isSQLIdentifierContinuation(character byte) bool {
	return isSQLIdentifierStart(character) ||
		character >= '0' && character <= '9'
}

// isPostgresIdentifierContinuation extends the ASCII keyword scanner with the
// dollar sign accepted by PostgreSQL unquoted identifiers. Non-ASCII bytes are
// treated conservatively as identifier content so they cannot create a false
// dollar-quote boundary during this safety check.
func isPostgresIdentifierContinuation(character byte) bool {
	return isSQLIdentifierContinuation(character) ||
		character == '$' ||
		character >= 0x80
}
