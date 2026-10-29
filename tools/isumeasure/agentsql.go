package main

import (
	"regexp"
	"strings"
)

// agentsql.go: the measurement agent's own statements (SHOW GLOBAL STATUS every second, processlist snapshots,
// row counts, the catalog, EXPLAIN) land in the same slow log as the app's. They must never show up as "the top
// SQL", an N+1 suspect or an action. The agent prefixes everything it sends with agentSQLTag (and runs the mysql
// CLI with --comments so the comment reaches the server); statements from older agents, and the mysql CLI's own
// `select @@version_comment`, are caught by agentSQLRe.

const agentSQLTagName = "isumeasure"
const agentSQLTag = "/* " + agentSQLTagName + " */ "

// agentSQL prefixes a statement the agent sends with the tag the report filters on.
func agentSQL(q string) string { return agentSQLTag + q }

// mysqlArgs: the mysql CLI flags for one agent statement (--comments keeps the tag; the CLI strips comments by default).
func mysqlArgs(flags string, q string) []string {
	return []string{"--comments", flags, agentSQL(q)}
}

const agentSQLHead = `SHOW\s+(?:GLOBAL\s+|SESSION\s+)?(?:STATUS|VARIABLES)\b` +
	`|SHOW\s+ENGINE\b` +
	`|SHOW\s+(?:FULL\s+)?PROCESSLIST\b` +
	`|FLUSH\s+(?:SLOW\s+)?LOGS\b` +
	`|EXPLAIN\b` +
	`|select\s+@@version_comment\b`
const agentSQLInfo = `\binformation_schema\.(?:processlist|tables|statistics|columns)\b`

var agentSQLRe = regexp.MustCompile(`(?is)^\s*(?:` + agentSQLHead +
	"|SELECT\\s+COUNT\\(\\*\\)\\s+FROM\\s+`[^`]+`\\.`[^`]+`\\s*;?\\s*$" +
	`)|` + agentSQLInfo)

// the two halves of agentSQLRe, so the unanchored information_schema search only runs on statements that mention it
var agentSQLStartRe = regexp.MustCompile(`(?is)^\s*(?:` + agentSQLHead +
	"|SELECT\\s+COUNT\\(\\*\\)\\s+FROM\\s+`[^`]+`\\.`[^`]+`\\s*;?\\s*$" + `)`)
var agentSQLInfoRe = regexp.MustCompile(`(?is)` + agentSQLInfo)

// agentSQLPrefixRe: the test for long statements, run on their first agentSQLPrefix bytes only (the agent's own
// statements are short; a 300 KB INSERT with a blob must not be scanned by a regexp just to learn it is not).
var agentSQLPrefixRe = regexp.MustCompile(`(?is)^\s*(?:` + agentSQLHead + `)|` + agentSQLInfo)

const agentSQLPrefix = 256

// isAgentSQL: tag is the leading comment (splitTag), sql the statement without it. Same answer as agentSQLRe.
func isAgentSQL(tag, sql string) bool {
	if tag == agentSQLTagName {
		return true
	}
	s := strings.TrimSpace(sql)
	if len(s) > 4*agentSQLPrefix {
		return agentSQLPrefixRe.MatchString(s[:agentSQLPrefix])
	}
	if agentSQLStartRe.MatchString(s) {
		return true
	}
	return containsFold(s, "information_schema.") && agentSQLInfoRe.MatchString(s)
}

// containsFold: case-insensitive strings.Contains for an ASCII needle, without allocating.
func containsFold(s, sub string) bool {
	c0 := sub[0] | 0x20
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i]|0x20 == c0 && strings.EqualFold(s[i:i+len(sub)], sub) {
			return true
		}
	}
	return false
}
