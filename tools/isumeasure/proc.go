package main

import (
	"bufio"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// sysRaw holds cumulative counters read from /proc; rates are derived between two samples.
type sysRaw struct {
	t                                   time.Time
	cpuUser, cpuSys, cpuIdle, cpuIOWait uint64
	cpuTotal                            uint64
	memTotalKB, memAvailKB              uint64
	load1, load5                        float64
	diskReadSectors, diskWriteSectors   uint64
	diskIOms                            uint64
	netRxBytes, netTxBytes              uint64
	tcpInUse, tcpTimeWait, tcpAlloc     uint64
	ctxt                                uint64
	procsRunning                        uint64
	diskFreeGB                          float64
	listenOverflows                     uint64 // /proc/net/netstat TcpExt (cumulative)
	portRange                           uint64 // size of net.ipv4.ip_local_port_range
}

var diskDevRe = regexp.MustCompile(`^(sd[a-z]+|xvd[a-z]+|vd[a-z]+|nvme\d+n\d+)$`)

func readSys() sysRaw {
	r := sysRaw{t: time.Now()}
	if f, err := os.Open("/proc/stat"); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fs := strings.Fields(sc.Text())
			if len(fs) < 2 {
				continue
			}
			switch fs[0] {
			case "cpu":
				v := make([]uint64, 0, 10)
				for _, s := range fs[1:] {
					n, _ := strconv.ParseUint(s, 10, 64)
					v = append(v, n)
				}
				if len(v) >= 5 {
					r.cpuUser, r.cpuSys, r.cpuIdle, r.cpuIOWait = v[0]+v[1], v[2], v[3], v[4]
					for _, n := range v {
						r.cpuTotal += n
					}
				}
			case "ctxt":
				r.ctxt, _ = strconv.ParseUint(fs[1], 10, 64)
			case "procs_running":
				r.procsRunning, _ = strconv.ParseUint(fs[1], 10, 64)
			}
		}
		f.Close()
	}
	if f, err := os.Open("/proc/meminfo"); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fs := strings.Fields(sc.Text())
			if len(fs) < 2 {
				continue
			}
			n, _ := strconv.ParseUint(fs[1], 10, 64)
			switch fs[0] {
			case "MemTotal:":
				r.memTotalKB = n
			case "MemAvailable:":
				r.memAvailKB = n
			}
		}
		f.Close()
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		fs := strings.Fields(string(b))
		if len(fs) >= 2 {
			r.load1, _ = strconv.ParseFloat(fs[0], 64)
			r.load5, _ = strconv.ParseFloat(fs[1], 64)
		}
	}
	if f, err := os.Open("/proc/diskstats"); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fs := strings.Fields(sc.Text())
			if len(fs) < 14 || !diskDevRe.MatchString(fs[2]) {
				continue
			}
			rs, _ := strconv.ParseUint(fs[5], 10, 64)
			ws, _ := strconv.ParseUint(fs[9], 10, 64)
			io, _ := strconv.ParseUint(fs[12], 10, 64)
			r.diskReadSectors += rs
			r.diskWriteSectors += ws
			r.diskIOms += io
		}
		f.Close()
	}
	if f, err := os.Open("/proc/net/dev"); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			i := strings.Index(line, ":")
			if i < 0 {
				continue
			}
			name := strings.TrimSpace(line[:i])
			if name == "lo" {
				continue
			}
			fs := strings.Fields(line[i+1:])
			if len(fs) < 9 {
				continue
			}
			rx, _ := strconv.ParseUint(fs[0], 10, 64)
			tx, _ := strconv.ParseUint(fs[8], 10, 64)
			r.netRxBytes += rx
			r.netTxBytes += tx
		}
		f.Close()
	}
	if b, err := os.ReadFile("/proc/net/netstat"); err == nil { // TcpExt: header line then values
		lines := strings.Split(string(b), "\n")
		for i := 0; i+1 < len(lines); i += 2 {
			if !strings.HasPrefix(lines[i], "TcpExt:") {
				continue
			}
			hs, vs := strings.Fields(lines[i]), strings.Fields(lines[i+1])
			for j := range hs {
				if hs[j] == "ListenOverflows" && j < len(vs) {
					r.listenOverflows, _ = strconv.ParseUint(vs[j], 10, 64)
				}
			}
		}
	}
	if b, err := os.ReadFile("/proc/sys/net/ipv4/ip_local_port_range"); err == nil {
		if fs := strings.Fields(string(b)); len(fs) == 2 {
			lo, _ := strconv.ParseUint(fs[0], 10, 64)
			hi, _ := strconv.ParseUint(fs[1], 10, 64)
			if hi > lo {
				r.portRange = hi - lo
			}
		}
	}
	if b, err := os.ReadFile("/proc/net/sockstat"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if !strings.HasPrefix(line, "TCP:") {
				continue
			}
			fs := strings.Fields(line)
			for i := 1; i+1 < len(fs); i += 2 {
				n, _ := strconv.ParseUint(fs[i+1], 10, 64)
				switch fs[i] {
				case "inuse":
					r.tcpInUse = n
				case "tw":
					r.tcpTimeWait = n
				case "alloc":
					r.tcpAlloc = n
				}
			}
		}
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err == nil {
		r.diskFreeGB = float64(st.Bavail) * float64(st.Bsize) / 1e9
	}
	return r
}

// sysSample is one row of the sys timeline (rates per second, gauges as-is).
type sysSample struct {
	T           string  `json:"t"`
	CPUUser     float64 `json:"cpu_user_pct"`
	CPUSys      float64 `json:"cpu_sys_pct"`
	CPUIOWait   float64 `json:"cpu_iowait_pct"`
	CPUIdle     float64 `json:"cpu_idle_pct"`
	Load1       float64 `json:"load1"`
	MemAvailMB  uint64  `json:"mem_avail_mb"`
	MemUsedPct  float64 `json:"mem_used_pct"`
	DiskReadMB  float64 `json:"disk_read_mb_s"`
	DiskWriteMB float64 `json:"disk_write_mb_s"`
	DiskUtilPct float64 `json:"disk_util_pct"`
	NetRxMB     float64 `json:"net_rx_mb_s"`
	NetTxMB     float64 `json:"net_tx_mb_s"`
	TCPInUse    uint64  `json:"tcp_inuse"`
	TCPTimeWait uint64  `json:"tcp_timewait"`
	CtxtPerSec  float64 `json:"ctxt_per_s"`
	ListenOvfPS float64 `json:"listen_overflows_per_s"`
	PortRange   uint64  `json:"port_range"`
	ProcsRun    uint64  `json:"procs_running"`
	DiskFreeGB  float64 `json:"disk_free_gb"`
}

func diffSys(a, b sysRaw) sysSample {
	dt := b.t.Sub(a.t).Seconds()
	if dt <= 0 {
		dt = 1
	}
	tot := float64(b.cpuTotal - a.cpuTotal)
	pct := func(x uint64) float64 {
		if tot <= 0 {
			return 0
		}
		return round1(100 * float64(x) / tot)
	}
	s := sysSample{
		T:           b.t.Format(time.RFC3339Nano),
		CPUUser:     pct(b.cpuUser - a.cpuUser),
		CPUSys:      pct(b.cpuSys - a.cpuSys),
		CPUIOWait:   pct(b.cpuIOWait - a.cpuIOWait),
		CPUIdle:     pct(b.cpuIdle - a.cpuIdle),
		Load1:       b.load1,
		MemAvailMB:  b.memAvailKB / 1024,
		DiskReadMB:  round2(float64(b.diskReadSectors-a.diskReadSectors) * 512 / 1e6 / dt),
		DiskWriteMB: round2(float64(b.diskWriteSectors-a.diskWriteSectors) * 512 / 1e6 / dt),
		DiskUtilPct: round1(float64(b.diskIOms-a.diskIOms) / (dt * 10)),
		NetRxMB:     round2(float64(b.netRxBytes-a.netRxBytes) / 1e6 / dt),
		NetTxMB:     round2(float64(b.netTxBytes-a.netTxBytes) / 1e6 / dt),
		TCPInUse:    b.tcpInUse,
		TCPTimeWait: b.tcpTimeWait,
		CtxtPerSec:  round1(float64(b.ctxt-a.ctxt) / dt),
		ListenOvfPS: round1(float64(b.listenOverflows-a.listenOverflows) / dt),
		PortRange:   b.portRange,
		ProcsRun:    b.procsRunning,
		DiskFreeGB:  round2(b.diskFreeGB),
	}
	if b.memTotalKB > 0 {
		s.MemUsedPct = round1(100 * float64(b.memTotalKB-b.memAvailKB) / float64(b.memTotalKB))
	}
	return s
}

func round1(x float64) float64 { return float64(int(x*10+0.5)) / 10 }
func round2(x float64) float64 { return float64(int(x*100+0.5)) / 100 }

// ---- per-process CPU (top consumers) ----

type procRaw struct {
	name  string
	ticks uint64
	rssMB uint64
}

type procSample struct {
	Name   string  `json:"name"`
	PID    int     `json:"pid"`
	CPUPct float64 `json:"cpu_pct"`
	RSSMB  uint64  `json:"rss_mb"`
}

func readProcs() map[int]procRaw {
	out := map[int]procRaw{}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return out
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		st := string(b)
		i := strings.LastIndexByte(st, ')')
		if i < 0 {
			continue
		}
		name := st[strings.IndexByte(st, '(')+1 : i]
		fs := strings.Fields(st[i+2:])
		if len(fs) < 22 {
			continue
		}
		ut, _ := strconv.ParseUint(fs[11], 10, 64)
		stt, _ := strconv.ParseUint(fs[12], 10, 64)
		rss, _ := strconv.ParseUint(fs[21], 10, 64)
		out[pid] = procRaw{name: name, ticks: ut + stt, rssMB: rss * 4096 / 1024 / 1024}
	}
	return out
}

// diffProcs returns the top-N processes by CPU over the interval (percent of one core; 200 = two cores busy).
func diffProcs(a, b map[int]procRaw, dt float64, n int) []procSample {
	var out []procSample
	for pid, cur := range b {
		prev, ok := a[pid]
		if !ok || prev.name != cur.name || cur.ticks < prev.ticks {
			continue
		}
		pct := float64(cur.ticks-prev.ticks) / dt // ticks are 1/100 s → percent of a core
		if pct < 0.5 {
			continue
		}
		out = append(out, procSample{Name: cur.name, PID: pid, CPUPct: round1(pct), RSSMB: cur.rssMB})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CPUPct > out[j].CPUPct })
	if len(out) > n {
		out = out[:n]
	}
	return out
}
