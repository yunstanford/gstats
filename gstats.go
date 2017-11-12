package main

import (
    "fmt"
    "os"
    "net"
    "syscall"
    "bufio"
    "io"
    "time"
    "flag"
    "sync"
    "strings"
    "strconv"
    "io/ioutil"
    "hash/fnv"
    "github.com/rcrowley/goagain"
    "github.com/BurntSushi/toml"
)

/////////////
// Consts  //
/////////////
const (
    // Server Listening Addr
    CONN_HOST = "localhost"
    CONN_PORT = ":3333"
    CONN_TYPE = "tcp"
    UDP_PORT = ":7777"

    // Backend Server Addr
    GRAPHITE_ADDR = "localhost:2003"

    // Messege types
    TYPE_KV = "kv"
    TYPE_COUNTER = "c"
    TYPE_GAUGE = "g"
    TYPE_TIMER = "ms"
    TYPE_HISTOGRAM = "h"

    // MSG_TEMPLATE
    // MSG_TEMPLATE = `{{.metric}} {{.value}} {{.timpstamp}}`
)

/////////////
// Usage   //
/////////////

func usage() {
    fmt.Fprintln(
        os.Stderr,
        "Usage: gstats -config=<path-to-config-file>",
    )
}

/////////////
// Config  //
/////////////
type commonConfig struct {
    FlushInterval     time.Duration
    TcpPort           string
    UdpPort           string
    GraphiteAddr      string
    NumBuckets        uint32
}

type Config struct {
    Common       commonConfig
    // Put any other configs here...
}

func NewConfig() *Config {
    cfg := &Config{
        Common: commonConfig{
            FlushInterval: 60,
            TcpPort: CONN_PORT,
            UdpPort: UDP_PORT,
            GraphiteAddr: GRAPHITE_ADDR,
            NumBuckets: 1,
        },
    }
    return cfg
}

func ParseConfigFile(file string) (*Config, error) {
    cfg := NewConfig()

    if file != "" {
        bytes, err := ioutil.ReadFile(file)
        if err != nil {
            return nil, err
        }
        body := string(bytes)

        if _, err := toml.Decode(body, cfg); err != nil {
            return nil, err
        }
    }
    return cfg, nil
}

////////////////////
// Hash Function  //
////////////////////

func hash(s string) uint32 {
    h := fnv.New32a()
    h.Write([]byte(s))
    return h.Sum32()
}

func getBucket(s string, numBuckets uint32) uint32 {
    return hash(s) % numBuckets
}

//////////////////////
// Metric Types     //
//////////////////////
type Counter struct {
    count   int
}

func (c *Counter) inc(num int) {
    c.count = c.count + num
}

type Gauge struct {
    value   float64
}

func (g *Gauge) set(val float64) {
    g.value = val
}

type Timer struct {

}

type Histogram struct {

}

type KeyValue struct {

}


/////////////
// Bucket  //
/////////////
type Bucket struct {
    // Data
    couters         map[string]*Counter
    gauges          map[string]*Gauge
    timers          map[string]*Timer
    histograms      map[string]*Histogram
    kvs             map[string]*KeyValue

    // Lock
    counterLock     sync.Mutex
    gaugeLock       sync.Mutex
    timerLock       sync.Mutex
    histogramLock   sync.Mutex
    kvsLock         sync.Mutex
}


func NewBucket() (*Bucket) {
    return &Bucket{
        couters: make(map[string]*Counter),
        gauges: make(map[string]*Gauge),
        timers: make(map[string]*Timer),
        histograms: make(map[string]*Histogram),
        kvs: make(map[string]*KeyValue),
    }
}

func AddCounterSample(bucket *Bucket, key string, val string) {
    value, _ := strconv.Atoi(val)

    bucket.counterLock.Lock()
    defer bucket.counterLock.Unlock()

    if _, ok := bucket.couters[key]; ok {
        // Exists
        bucket.couters[key].inc(value)
        fmt.Println("Received a Counter (exists)!")
    } else {
        // Not Exist
        bucket.couters[key] = &Counter {
            count: value,
        }
        fmt.Println("Received a Counter (dont exists)!")
    }
}

func AddGaugeSample(bucket *Bucket, key string, val string) {
    value, _ := strconv.ParseFloat(val, 64)

    bucket.gaugeLock.Lock()
    defer bucket.gaugeLock.Unlock()

    if _, ok := bucket.gauges[key]; ok {
        // Exists
        bucket.gauges[key].set(value)
        fmt.Println("Received a Gauge (exists)!")
    } else {
        // Not Exist
        bucket.gauges[key] = &Gauge {
            value: value,
        }
        fmt.Println("Received a Gauge (dont exists)!")
    }
}

func AddTimerSample(bucket *Bucket, key string, val string) {

}

func AddHistogramSample(bucket *Bucket, key string, val string) {

}

func AddKVSample(bucket *Bucket, key string, val string) {

}

////////////////
// Data Sink  //
////////////////
type DataSink struct {
    conn               *net.TCPConn
    flushInterval      time.Duration
    // Needs some more design...
    bucket             *Bucket
}

func NewDataSink(addr string, flushInterval time.Duration, numBuckets uint32) (*DataSink, error) {
    // Initialize a Conn to backend server
    tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
    if err != nil {
        fmt.Println("ResolveTCPAddr failed:", err.Error())
        os.Exit(1)
    }

    conn, err := net.DialTCP("tcp", nil, tcpAddr)
    if err != nil {
        fmt.Println("Dial failed:", err.Error())
        os.Exit(1)
    }

    // Initialize a new Bucket
    bucket := NewBucket()

    // Initialize Data Sink
    sink := DataSink{
        conn: conn,
        flushInterval: flushInterval * time.Second,
        bucket: bucket,
    }

    // Start flush goroutine
    go sink.handleFlush()

    return &sink, nil
}

func (ds *DataSink) handleFlush() {
    // Init a ticker
    flushTicker := time.NewTicker(ds.flushInterval)
    for {
        <- flushTicker.C
        // Time Now
        now := time.Now()
        epochNow := now.Unix()
        fmt.Println(epochNow)

        // Swap and buffer
        old_bucket := ds.bucket
        ds.bucket = NewBucket()

        // Spawn a goroutine for flushing
        go flushBucket(old_bucket, ds.conn, epochNow)
    }
}

func flushCounters(old_bucket *Bucket, conn *net.TCPConn, epochNow int64) {
    fmt.Println("Flush Counters...")
    for k, c := range old_bucket.couters {
        msg := k + " " + strconv.Itoa(c.count) + " " + strconv.Itoa(int(epochNow)) + "\n"
        fmt.Println("Flushing msg: " + msg)
        conn.Write([]byte(msg))
        fmt.Println("Flushed msg: " + msg)
    }
}

func flushGauges(old_bucket *Bucket, conn *net.TCPConn, epochNow int64) {
    fmt.Println("Flush Gauges...")
    for k, g := range old_bucket.gauges {
        msg := k + " " + strconv.FormatFloat(g.value, 'f', -1, 64) + " " + strconv.Itoa(int(epochNow)) + "\n"
        fmt.Println("Flushing msg: " + msg)
        conn.Write([]byte(msg))
        fmt.Println("Flushed msg: " + msg)
    }
}

func flushTimers(old_bucket *Bucket, conn *net.TCPConn, epochNow int64) {

}

func flushHistograms(old_bucket *Bucket, conn *net.TCPConn, epochNow int64) {

}

func flushKVs(old_bucket *Bucket, conn *net.TCPConn, epochNow int64) {

}

func flushBucket(old_bucket *Bucket, conn *net.TCPConn, epochNow int64) {
    // Flush counters
    go flushCounters(old_bucket, conn, epochNow)

    // Flush gauges
    go flushGauges(old_bucket, conn, epochNow)

    // Flush timers

    // Flush histograms

    // Flush kvs
}

///////////////////////////
// Parse and Validation  //
///////////////////////////
func parse(buf []byte) (string, string, string) {
    line := string(buf)
    tmp := strings.Split(line, "|")
    kv, msgType := tmp[0], tmp[1]

    tmp = strings.Split(kv, ":")
    key, val := tmp[0], tmp[1]

    return key, val, msgType
}

/////////////
// Server  //
/////////////

// Accept TCP Conns
func acceptTCPConn(l *net.TCPListener, ds *DataSink) {
    for {
        // Listen for an incoming connection.
        conn, err := l.Accept()
        if err != nil {
            fmt.Println("Error accepting: ", err.Error())
            os.Exit(1)
        }
        // Handle connections in a new goroutine.
        go handleRequest(conn, ds)
    }
}

// Handles incoming requests.
func handleRequest(conn net.Conn, ds *DataSink) {
    // Close the connection when you're done with it.
    defer conn.Close()

    r := bufio.NewReaderSize(conn, 4096)
    // DONOT block data processing, keep it go fast.
    for {
        buf, _, err := r.ReadLine()
        if err != nil {
            if io.EOF != err {
                fmt.Println("Errs out")
            }
            break
        }

        // Validate and parse incoming data
        key, value, msgType := parse(buf)
        fmt.Println("Key: " + key)
        fmt.Println("Value: " + value)
        fmt.Println("Type: " + msgType)

        // Feed into data buckets pool
        switch msgType {
            case TYPE_COUNTER:
                AddCounterSample(ds.bucket, key, value)
            case TYPE_HISTOGRAM:
                AddHistogramSample(ds.bucket, key, value)
            case TYPE_TIMER:
                AddTimerSample(ds.bucket, key, value)
            case TYPE_KV:
                AddKVSample(ds.bucket, key, value)
            case TYPE_GAUGE:
                AddGaugeSample(ds.bucket, key, value)
        }

        fmt.Println("Processed!")
    }
}

///////////
// Main  //
///////////

func main() {

    // Command line flags
    configFile := flag.String("config", "", "config filename")
    flag.Parse()

    // Parse Config File...
    cfg, err := ParseConfigFile(*configFile)
    if err != nil {
        usage()
        return
    }

    // Initialize Data Sink
    dataSink, err := NewDataSink(cfg.Common.GraphiteAddr, cfg.Common.FlushInterval, cfg.Common.NumBuckets)

    // TCP
    // Listen for incoming connections.
    l, ppid, err := goagain.GetEnvs()
    if nil != err {
        laddr, err := net.ResolveTCPAddr("tcp", cfg.Common.TcpPort)
        if nil != err {
            os.Exit(1)
        }
        l, err = net.ListenTCP("tcp", laddr)
        if nil != err {
            os.Exit(1)
        }
        go acceptTCPConn(l.(*net.TCPListener), dataSink)
    } else {
        go acceptTCPConn(l.(*net.TCPListener), dataSink)
        if err := goagain.KillParent(ppid); nil != err {
            os.Exit(1)
        }
        for {
            err := syscall.Kill(ppid, 0)
            if err != nil {
                break
            }
            time.Sleep(10 * time.Millisecond)
        }
    }
    fmt.Println("TCP Listening on " + cfg.Common.TcpPort)

    // UDP
    udp_addr, err := net.ResolveUDPAddr("udp", cfg.Common.UdpPort)
    if nil != err {
        fmt.Println("Errs out when resolving udp server.")
        os.Exit(1)
    }
    udp_conn, err := net.ListenUDP("udp", udp_addr)

    defer udp_conn.Close()

    fmt.Println("UDP Listening on " + cfg.Common.UdpPort)
    if nil != err {
        fmt.Println("Errs out when starting udp server.")
        os.Exit(1)
    }

    go handleRequest(udp_conn, dataSink)


    // Block the main goroutine awaiting signals.

    if err := goagain.AwaitSignals(l); nil != err {
        os.Exit(1)
    }

    fmt.Println("Going to stop...")
}
