# python3 -m unittest tools/lanes/test_indexcheck.py — task index:check (tools/indexcheck/indexcheck.py).
# The Go fixtures are cut down from the real cases: ISUCON14 coupons(code) (2026-10-27) and coupons(used_by)
# (2026-10-28, 5xx on POST /api/app/users and /api/app/rides), ISUCON13 icons(user_id) (2026-10-25, 500 x123) and the
# read-only ones that must stay quiet (livestream_tags(livestream_id), reservation_slots, icons after the PK fix).
import os
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
TOOL = os.path.join(os.path.dirname(HERE), "indexcheck", "indexcheck.py")
sys.path.insert(0, os.path.dirname(TOOL))
import indexcheck as ic  # noqa: E402

APP_HANDLERS = '''package main

func appPostUsers(w http.ResponseWriter, r *http.Request) {
	tx, err := db.Beginx()
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx,
		"INSERT INTO users (id, username, invitation_code) VALUES (?, ?, ?)",
		userID, req.Username, invitationCode)
	_, err = tx.ExecContext(ctx,
		"INSERT INTO coupons (user_id, code, discount) VALUES (?, ?, ?)",
		userID, "CP_NEW2024", 3000)
	if req.InvitationCode != nil {
		var coupons []Coupon
		err = tx.SelectContext(ctx, &coupons, "SELECT * FROM coupons WHERE code = ? FOR UPDATE", "INV_"+*req.InvitationCode)
		var inviter User
		err = tx.GetContext(ctx, &inviter, "SELECT * FROM users WHERE invitation_code = ?", *req.InvitationCode)
		_, err = tx.ExecContext(ctx,
			"INSERT INTO coupons (user_id, code, discount) VALUES (?, ?, ?)",
			userID, "INV_"+*req.InvitationCode, 1500)
	}
	tx.Commit()
}

func appPostRides(w http.ResponseWriter, r *http.Request) {
	tx, err := db.Beginx()
	if _, err := tx.ExecContext(ctx, `INSERT INTO rides (id, user_id) VALUES (?, ?)`, rideID, user.ID); err != nil {
	}
	var coupon Coupon
	if err := tx.GetContext(ctx, &coupon, "SELECT * FROM coupons WHERE user_id = ? AND used_by IS NULL ORDER BY created_at LIMIT 1 FOR UPDATE", user.ID); err != nil {
	} else {
		if _, err := tx.ExecContext(ctx,
			"UPDATE coupons SET used_by = ? WHERE user_id = ? AND code = ?",
			rideID, user.ID, coupon.Code); err != nil {
		}
	}
	tx.Commit()
}

func appGetRides(w http.ResponseWriter, r *http.Request) {
	rows := []Ride{}
	db.SelectContext(ctx, &rows, `SELECT * FROM rides WHERE user_id = ? ORDER BY created_at DESC`, user.ID)
	db.GetContext(ctx, &coupon, "SELECT * FROM coupons WHERE used_by = ?", ride.ID)
}
'''

ISUPIPE = '''package main

func postIconHandler(c echo.Context) error {
	tx, err := dbConn.BeginTxx(ctx, nil)
	if _, err := tx.ExecContext(ctx, "DELETE FROM icons WHERE user_id = ?", userID); err != nil {
	}
	rs, err := tx.ExecContext(ctx, "INSERT INTO icons (user_id, image) VALUES (?, ?)", userID, req.Image)
	tx.Commit()
}

func reserveLivestreamHandler(c echo.Context) error {
	tx, err := dbConn.BeginTxx(ctx, nil)
	var slots []*ReservationSlotModel
	if err := tx.SelectContext(ctx, &slots, "SELECT * FROM reservation_slots WHERE start_at >= ? AND end_at <= ? FOR UPDATE", req.StartAt, req.EndAt); err != nil {
	}
	if _, err := tx.ExecContext(ctx, "UPDATE reservation_slots SET slot = slot - 1 WHERE start_at >= ? AND end_at <= ?", req.StartAt, req.EndAt); err != nil {
	}
	rs, err := tx.NamedExecContext(ctx, "INSERT INTO livestreams (user_id, title) VALUES(:user_id, :title)", lm)
	for _, tagID := range req.Tags {
		if _, err := tx.NamedExecContext(ctx, "INSERT INTO livestream_tags (livestream_id, tag_id) VALUES (:livestream_id, :tag_id)", &LivestreamTagModel{}); err != nil {
		}
	}
	tx.Commit()
}

func getLivestreamHandler(c echo.Context) error {
	tx, err := dbConn.BeginTxx(ctx, nil)
	if err := tx.SelectContext(ctx, &tags, "SELECT * FROM livestream_tags WHERE livestream_id = ?", id); err != nil {
	}
	tx.Commit()
}
'''

ISUPIPE_FIXED = ISUPIPE.replace(
    '"DELETE FROM icons WHERE user_id = ?", userID',
    '"DELETE FROM icons WHERE id IN (?)", ids').replace(
    'tx, err := dbConn.BeginTxx(ctx, nil)\n\tif _, err := tx.ExecContext(ctx, "DELETE',
    'tx, err := dbConn.BeginTxx(ctx, nil)\n\ttx.SelectContext(ctx, &ids, "SELECT id FROM icons WHERE user_id = " +\n\t\t"?", userID)\n\tif _, err := tx.ExecContext(ctx, "DELETE')


def run(files, code, *extra):
    with tempfile.TemporaryDirectory() as d:
        code_dir = os.path.join(d, "webapp/go")
        os.makedirs(code_dir)
        for name, text in code.items():
            with open(os.path.join(code_dir, name), "w") as f:
                f.write(text)
        paths = []
        for i, sql in enumerate(files):
            p = os.path.join(d, "idx%d.sql" % i)
            with open(p, "w") as f:
                f.write(sql)
            paths.append(p)
        r = subprocess.run([sys.executable, TOOL, "--code-dir", "webapp/go", *extra, *paths], cwd=d, capture_output=True, text=True)
        return r.returncode, r.stdout + r.stderr


class ParseTest(unittest.TestCase):
    def test_alter_create_unique_backticks_prefix_comments(self):
        sql = """-- report x: coupons(code) full scan
ALTER TABLE coupons ADD INDEX idx_coupons_code (code);
ALTER TABLE `rides` ADD INDEX `idx_a` (`chair_id`, `updated_at` DESC), ADD UNIQUE KEY uk (user_id(10));
CREATE INDEX idx_x ON chair_locations (chair_id, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS u ON t1(a);
/* ALTER TABLE nope ADD INDEX n (z); */
ALTER TABLE icons ADD COLUMN hash CHAR(64) NOT NULL DEFAULT '';
"""
        got = [(t, c) for t, c, _ in ic.parse_indexes(sql)]
        self.assertEqual(got, [("coupons", ["code"]), ("rides", ["chair_id", "updated_at"]), ("rides", ["user_id"]),
                               ("chair_locations", ["chair_id", "created_at"]), ("t1", ["a"])])

    def test_sql_literals_lines_and_concatenation(self):
        src = 'func f() {\n\ta := 1\n\ttx.Exec("SELECT * FROM t " +\n\t\t"WHERE x = ? FOR UPDATE")\n\tdb.Exec(`UPDATE t\n SET y = 1`)\n}\n'
        st = ic.scan_code([("a.go", src)])
        self.assertEqual([(s.line, s.kind, s.locking) for s in st], [(3, "SELECT", True), (5, "UPDATE", False)])
        self.assertEqual(st[0].sql, "SELECT * FROM t WHERE x = ? FOR UPDATE")


class CheckTest(unittest.TestCase):
    def test_coupons_code_isucon14_1027(self):
        rc, out = run(["ALTER TABLE coupons ADD INDEX idx_coupons_code (code);\n"], {"app_handlers.go": APP_HANDLERS})
        self.assertEqual(rc, 2, out)
        self.assertIn("!! coupons(code) — deadlock risk", out)
        self.assertRegex(out, r"\[b\] .*app_handlers.go:14 \(appPostUsers\)  SELECT \* FROM coupons WHERE code = \? FOR UPDATE   <- locks by code, then INSERT INTO coupons at :18")
        self.assertNotIn("appPostRides", out)  # its UPDATE … WHERE code = ? is the last write of the tx: holds, never waits
        self.assertIn("IMPLEMENTATION-NOTES.md §5", out)
        self.assertIn("index-check: reviewed", out)

    def test_coupons_used_by_isucon14_1028(self):
        rc, out = run(["ALTER TABLE coupons ADD INDEX idx_coupons_used_by (used_by);\n"], {"app_handlers.go": APP_HANDLERS})
        self.assertEqual(rc, 2, out)
        self.assertIn("!! coupons(used_by)", out)
        self.assertRegex(out, r"\[b\] .*:29 \(appPostRides\).*FOR UPDATE   <- locks by used_by, then UPDATE … SET used_by on coupons at :32")
        self.assertRegex(out, r"\[c\] .*:10 \(appPostUsers\)  INSERT INTO coupons .*<- INSERT after :7 in the same tx")
        self.assertNotIn("appGetRides", out)  # autocommit read by used_by: no lock

    def test_icons_replace_by_delete_isucon13(self):
        rc, out = run(["ALTER TABLE icons ADD INDEX idx_icons_user_id (user_id);\n"], {"user_handler.go": ISUPIPE})
        self.assertEqual(rc, 2, out)
        self.assertRegex(out, r"\[a\] .*:5 \(postIconHandler\)  DELETE FROM icons WHERE user_id = \?   <- replace-by-delete: then INSERT INTO icons at :7")

    def test_quiet_cases(self):
        sql = """ALTER TABLE livestream_tags ADD INDEX idx_livestream_tags_livestream_id (livestream_id);
ALTER TABLE reservation_slots ADD INDEX idx_reservation_slots_start_at_end_at (start_at, end_at);
ALTER TABLE icons ADD INDEX idx_icons_user_id (user_id);
ALTER TABLE rides ADD INDEX idx_rides_user_id_created_at (user_id, created_at);
"""
        # livestream_tags: only read by livestream_id; reservation_slots: never inserted; icons: DELETE by primary key
        rc, out = run([sql], {"h.go": ISUPIPE_FIXED, "app_handlers.go": APP_HANDLERS.replace("coupons", "cpns")})
        self.assertEqual(rc, 0, out)
        self.assertIn("ok: no locking read", out)

    def test_no_index_lines_and_usage(self):
        rc, out = run(["ALTER TABLE icons ADD COLUMN hash CHAR(64);\n"], {"a.go": ISUPIPE})
        self.assertEqual((rc, "nothing to check" in out), (0, True), out)
        r = subprocess.run([sys.executable, TOOL], capture_output=True, text=True)
        self.assertEqual(r.returncode, 1)


class RefModeTest(unittest.TestCase):
    def test_branch_added_lines_and_code_at_the_branch(self):
        d = tempfile.mkdtemp()

        def g(*a):
            subprocess.run(["git", *a], cwd=d, check=True, capture_output=True)
        g("init", "-q", "-b", "main")
        g("config", "user.email", "t@example.com")
        g("config", "user.name", "t")
        os.makedirs(os.path.join(d, "webapp/go"))
        os.makedirs(os.path.join(d, "webapp/sql/indexes"))
        with open(os.path.join(d, "webapp/go/app_handlers.go"), "w") as f:
            f.write(APP_HANDLERS)
        with open(os.path.join(d, "webapp/sql/indexes/early.sql"), "w") as f:
            f.write("ALTER TABLE coupons ADD INDEX idx_coupons_code (code);\n")  # already on main: not this PR's
        g("add", "-A")
        g("commit", "-q", "-m", "base")
        g("checkout", "-q", "-b", "feat")
        with open(os.path.join(d, "webapp/sql/indexes/new.sql"), "w") as f:
            f.write("ALTER TABLE rides ADD INDEX idx_rides_user_id (user_id);\n")
        g("add", "-A")
        g("commit", "-q", "-m", "rides index")
        r = subprocess.run([sys.executable, TOOL, "--ref", "feat", "--base", "main"], cwd=d, capture_output=True, text=True)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("1 index(es): rides(user_id)", r.stdout)
        with open(os.path.join(d, "webapp/sql/indexes/new.sql"), "a") as f:
            f.write("ALTER TABLE coupons ADD INDEX idx_coupons_used_by (used_by);\n")
        g("commit", "-q", "-am", "used_by")
        g("checkout", "-q", "main")  # the work tree is main: the code is read from the ref, not the disk
        r = subprocess.run([sys.executable, TOOL, "--ref", "feat", "--base", "main"], cwd=d, capture_output=True, text=True)
        self.assertEqual(r.returncode, 2, r.stdout + r.stderr)
        self.assertIn("2 index(es): rides(user_id), coupons(used_by)", r.stdout)
        self.assertIn("!! coupons(used_by)", r.stdout)


if __name__ == "__main__":
    unittest.main()
