package main

import "testing"

const dlSample = `------------------------
LATEST DETECTED DEADLOCK
------------------------
2026-10-18 16:12:42 134936444102336
*** (1) TRANSACTION:
TRANSACTION 152618, ACTIVE 0 sec inserting
mysql tables in use 1, locked 1
MySQL thread id 446, OS thread handle 1, query id 475821 10.0.0.1 isucon update
/* POST /api/chair/coordinate @ chair_handlers.go:156 */ INSERT INTO chair_location_summaries (chair_id, latitude) VALUES ('01M', 42)

*** (1) HOLDS THE LOCK(S):
RECORD LOCKS space id 97 page no 7 n bits 264 index PRIMARY of table ` + "`isuride`.`chair_location_summaries`" + ` trx id 152618 lock_mode X locks gap before rec

*** (1) WAITING FOR THIS LOCK TO BE GRANTED:
RECORD LOCKS space id 97 page no 7 n bits 264 index PRIMARY of table ` + "`isuride`.`chair_location_summaries`" + ` trx id 152618 lock_mode X locks gap before rec insert intention waiting

*** (2) TRANSACTION:
TRANSACTION 152641, ACTIVE 0 sec inserting
MySQL thread id 461, OS thread handle 2, query id 476038 10.0.0.1 isucon update
/* POST /api/chair/coordinate @ chair_handlers.go:156 */ INSERT INTO chair_location_summaries (chair_id, latitude) VALUES ('01N', 337)

*** (2) WAITING FOR THIS LOCK TO BE GRANTED:
RECORD LOCKS space id 97 page no 7 n bits 264 index PRIMARY of table ` + "`isuride`.`chair_location_summaries`" + ` trx id 152641 lock_mode X locks gap before rec insert intention waiting

*** WE ROLL BACK TRANSACTION (2)
------------
TRANSACTIONS
------------
`

func TestParseDeadlock(t *testing.T) {
	dl := parseDeadlock(dlSample)
	if dl == nil || len(dl.txs) != 2 {
		t.Fatalf("parse: %+v", dl)
	}
	a, b := dl.txs[0], dl.txs[1]
	if a.tag != "POST /api/chair/coordinate" || a.loc != "chair_handlers.go:156" || a.norm != "INSERT INTO chair_location_summaries (chair_id, latitude) VALUES (...)" {
		t.Errorf("tx1: %+v", a)
	}
	if a.rolledBack || !b.rolledBack || a.waiting == "" {
		t.Errorf("rollback/waiting: %+v %+v", a, b)
	}
	if dl.at != "2026-10-18 16:12:42" || !dl.matches("POST /api/chair/coordinate") || dl.matches("GET /api/x") {
		t.Errorf("at/matches: %s", dl.at)
	}
}
