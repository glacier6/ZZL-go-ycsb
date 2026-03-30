# mixgraph
NOTE:2026033001 调配各区间内读写等比例
NOTE:2026033000 扔操作类型的骰子

# YCSB工作原理
YCSB（包括 go-ycsb）的工作原理其实非常像一个 “高并发的随机数据工厂”。它不是预先生成好 10GB 的文件然后导进去，而是在**运行时（Runtime）**利用算法动态生成数据，并实时发送给 BadgerDB。
NOTE:论文只用YCSB负载也是可以的！！
1.Key 是怎么产生的？(数学分布的魔法)
  格式：user + <数字 ID>
  实际的例子 usertableuser2738207479313280650
  requestdistribution 参数控制数字ID的分布范围：
  NOTE:注意这个数字ID也不是顺序增加的，YCSB本身有一个单调递增的计数器，默认是将这个计数器再进行HASH才会得出KEY的，这个HASH可以通过insertorder=ordered关掉！
  NOTE:特别注意,读取和更新在底层是共用的同一个概率分布,所以,这两种操作的key会高度重合!
    Uniform (均匀分布)：
      使用 Random(0, recordcount)。
      每个 Key 被选中的概率一样。
      效果：数据彻底打散，BadgerDB 的 Block Cache 命中率会很低。
    Zipfian (齐夫/幂律分布)：
      使用复杂的数学公式（Zipf 算法）。
      效果：它会故意死盯着某几个 ID疯狂访问，而其他的 ID 很少碰，NOTE:但需要注意的是，这个ID并不是局限在某一个KEY区域的！！！像雨点一样分散的！。
      模拟：微博热搜、秒杀商品。这对测试你的 缓存优化 至关重要。
    Latest (最新分布)：
      它偏向于选择 最大 的那些数字(NOTE:注意是看对key自增器hash之前的)。
      效果：模拟“看最新的帖子”。
    hotspot(热点聚集):
      热点会聚集在一起 NOTE:必须关掉HASH,使用insertorder=ordered来直接用key
    
TODO:还需要看看那些有关前缀的文章如何做测试的！！

2.Value 是怎么产生的？(随机字符串)
  YCSB 的 Value 没有任何实际业务含义，全是 随机生成的垃圾字符 (Random Garbage)，但它遵循固定的结构。

3.工作流：一个请求的诞生过程
  threadcount参数控制请求协程的个数，每个协程跑一个死循环，如果为Read，就按照上面那个分布随机取。如果是写入，同上

NOTE:特别注意！！每次ycsb打包会直接把所有需要的数据库程序直接打入自己的可执行程序，所以如果改了数据库代码，需要重新打一次ycsb的包！！
NOTE:pkg/generator下文件的功能就是选择读取与更新的key


# YCSB的各个主要参数
下面是启动样例命令
./bin/go-ycsb load badger -P workloads/workloada -P zzl_badger.properties -p recordcount=1000000 -p threadcount=16

./bin/go-ycsb run badger -P workloads/workloada -P zzl_badger.properties -p recordcount=1000000 -p operationcount=1000000 -p requestdistribution=zipfian -p threadcount=16

下面是参数（使用都要如上例加一个 -p）
1.执行控制类 (Execution Control)
    决定压测“怎么跑”、“跑多久”、“多少人跑”。
    参数名	            默认值	      含义与调优建议
    threadcount	        1	        并发线程数 (在 Go 中对应 Goroutines)。重要： 测 Badger 时，建议从 8, 16, 32, 64 依次递增，直到吞吐量不再上升（找到饱和点）。过高会导致上下文切换开销，过低无法喂饱磁盘。
    target	            无 (不限速)	 目标 QPS (Target Throughput)。用法： 如果你想测“在 1万 QPS 下的延迟是多少”，就设 target=10000。如果不设，则是“火力全开”测最大吞吐。
    maxexecutiontime	无	        最大运行时间 (秒)。用法： 比如 -p maxexecutiontime=600，强制跑 10 分钟后停止。这比用 operationcount 更适合做稳定性测试。

2.数据规模类 (Data Volume)
    决定数据库里“有多少数据”、“要操作多少次”。
    参数名	        默认值	含义与调优建议
    recordcount	    0	  数据库中的总记录数 (Load 阶段用)。重要： 这决定了 LSM-Tree 的层级深度。100万条和 1亿条，Badger 的性能表现完全不同。
    operationcount	0	  本次测试执行的操作总数 (Run 阶段用)。用法： 设大一点（如 1000万），让测试跑足够久，以便观察 Compaction 发生时的抖动。
    insertstart	    0	  Key 的起始偏移量。用法： 如果你先 Load 了 100万，想再 Load 100万（变成200万），第二次 Load 时设 insertstart=1000000。

3.数据形态类 (Data Shape)
    决定“Key 长什么样”、“Value 长什么样”。这直接关系到 Badger 的 KV 分离机制。
    参数名	                默认值	      含义与调优建议
    fieldcount	            10	        字段数量。即 Value 是一个包含多少个 Key 的 Map。
    fieldlength	            100	        每个字段的字节最大长度（单位是B）。
    fieldlengthdistribution	constant	Value 长度分布。
                                        （1）constant: 固定长度。
                                        （2）uniform: 均匀分布。
                                        （3）zipfian: 大小两极分化。
    readallfields	        true	    读取时是否返回所有字段。
                                        （1）true: 模拟取出完整对象（IO 压力大）。
                                        （2）false: 模拟只查对象的一个属性。

4.负载比例类 (Workload Mix)
    决定 CRUD 的混合比例。所有比例之和应为 1.0。
    参数名	              含义	对应 Workload
    readproportion	    读比例	几乎所有负载都会用到。
    updateproportion	更新比例 (修改现有 Key)	Workload A (0.5), B (0.05)
    insertproportion	插入比例 (新增 Key)	Workload D (0.05), E (0.05)
    scanproportion	    扫描比例 (范围查询)	Workload E (0.95)
    readmodifywriteproportion	读改写比例 (原子操作)	Workload F (0.5)

5.访问分布类 (Distribution - 核心灵魂)
    决定“访问哪些 Key”。这是测试缓存（Block Cache）和热点处理的关键。
    参数名	                 含义
    requestdistribution	    请求分布模式。
                            （1）uniform: 随机访问。最考验磁盘 IO，因为缓存很难命中。
                            （2）zipfian: 幂律分布（20% 热点）。最考验缓存策略。验证你的“冷热分离”必须用这个。
                            （3）latest: 总是读最近写入的。考验 MemTable/L0 性能。
    scanlength	            扫描长度 (用于 Scan 操作)。
                            （1）默认不固定。
                            （2）-p scanlength=100: 每次 Scan 固定扫 100 个 Key。验证你的“索引优化”时，调整这个值（短扫 vs 长扫）。
    insertorder	            Key 的插入顺序。
                            （1）ordered: 顺序插入 (user1, user2...)。对 LSM 最友好，写放大最小。•
                            （2）hashed: 哈希乱序插入。对 LSM 压力最大，写放大最高。

# go-ycsb

go-ycsb is a Go port of [YCSB](https://github.com/brianfrankcooper/YCSB). It fully supports all YCSB generators and the Core workload so we can do the basic CRUD benchmarks with Go.

## Why another Go YCSB?

+ We want to build a standard benchmark tool in Go.
+ We are not familiar with Java.

## Getting Started

### Download

https://github.com/pingcap/go-ycsb/releases/latest

**Linux**
```
wget -c https://github.com/pingcap/go-ycsb/releases/latest/download/go-ycsb-linux-amd64.tar.gz -O - | tar -xz

# give it a try
./go-ycsb --help
```

**OSX**
```
wget -c https://github.com/pingcap/go-ycsb/releases/latest/download/go-ycsb-darwin-amd64.tar.gz -O - | tar -xz

# give it a try
./go-ycsb --help
```

### Building from source

```bash
git clone https://github.com/pingcap/go-ycsb.git
cd go-ycsb
make

# give it a try
./bin/go-ycsb  --help
```

Notice:

+ Minimum supported go version is 1.16.
+ To use FoundationDB, you must install [client](https://www.foundationdb.org/download/) library at first, now the supported version is 6.2.11.
+ To use RocksDB, you must follow [INSTALL](https://github.com/facebook/rocksdb/blob/master/INSTALL.md) to install RocksDB at first.

## Usage

Mostly, we can start from the official document [Running-a-Workload](https://github.com/brianfrankcooper/YCSB/wiki/Running-a-Workload).

### Shell

```basic
./bin/go-ycsb shell basic
» help
YCSB shell command

Usage:
  shell [command]

Available Commands:
  delete      Delete a record
  help        Help about any command
  insert      Insert a record
  read        Read a record
  scan        Scan starting at key
  table       Get or [set] the name of the table
  update      Update a record
```

### Load

```bash
./bin/go-ycsb load basic -P workloads/workloada
```

### Run

```bash
./bin/go-ycsb run basic -P workloads/workloada
```

## Supported Database

- MySQL / TiDB
- TiKV
- FoundationDB
- Aerospike
- Badger
- Cassandra / ScyllaDB
- Pegasus
- PostgreSQL / CockroachDB / AlloyDB / Yugabyte
- RocksDB
- Spanner
- Sqlite
- MongoDB
- Redis and Redis Cluster
- BoltDB
- etcd
- DynamoDB
- S3 (Amazon S3 / S3-compatible)

## Output configuration

|field|default value|description|
|-|-|-|
|measurementtype|"histogram"|The mechanism for recording measurements, one of `histogram`, `raw` or `csv`|
|measurement.output_file|""|File to write output to, default writes to stdout|

## Database Configuration

You can pass the database configurations through `-p field=value` in the command line directly.

Common configurations:

|field|default value|description|
|-|-|-|
|dropdata|false|Whether to remove all data before test|
|verbose|false|Output the execution query|
|debug.pprof|":6060"|Go debug profile address|

### MySQL & TiDB

|field|default value|description|
|-|-|-|
|mysql.host|"127.0.0.1"|MySQL Host|
|mysql.port|3306|MySQL Port|
|mysql.user|"root"|MySQL User|
|mysql.password||MySQL Password|
|mysql.db|"test"|MySQL Database|
|tidb.cluster_index|true|Whether to use cluster index, for TiDB only|
|tidb.instances|""|Comma-seperated address list of tidb instances (eg: `tidb-0:4000,tidb-1:4000`)|


### TiKV

|field|default value|description|
|-|-|-|
|tikv.pd|"127.0.0.1:2379"|PD endpoints, seperated by comma|
|tikv.type|"raw"|TiKV mode, "raw", "txn", or "coprocessor"|
|tikv.conncount|128|gRPC connection count|
|tikv.batchsize|128|Request batch size|
|tikv.async_commit|true|Enalbe async commit or not|
|tikv.one_pc|true|Enable one phase or not|
|tikv.apiversion|"V1"|[api-version](https://docs.pingcap.com/tidb/stable/tikv-configuration-file#api-version-new-in-v610) of tikv server, "V1" or "V2"|

### FoundationDB

|field|default value|description|
|-|-|-|
|fdb.cluster|""|The cluster file used for FoundationDB, if not set, will use the [default](https://apple.github.io/foundationdb/administration.html#default-cluster-file)|
|fdb.dbname|"DB"|The cluster database name|
|fdb.apiversion|510|API version, now only 5.1 is supported|

### PostgreSQL & CockroachDB & AlloyDB & Yugabyte

|field|default value|description|
|-|-|-|
|pg.host|"127.0.0.1"|PostgreSQL Host|
|pg.port|5432|PostgreSQL Port|
|pg.user|"root"|PostgreSQL User|
|pg.password||PostgreSQL Password|
|pg.db|"test"|PostgreSQL Database|
|pg.sslmode|"disable|PostgreSQL ssl mode|

### Aerospike

|field|default value|description|
|-|-|-|
|aerospike.host|"localhost"|The port of the Aerospike service|
|aerospike.port|3000|The port of the Aerospike service|
|aerospike.ns|"test"|The namespace to use|

### Badger

|field|default value|description|
|-|-|-|
|badger.dir|"/tmp/badger"|The directory to save data|
|badger.valuedir|"/tmp/badger"|The directory to save value, if not set, use badger.dir|
|badger.sync_writes|false|Sync all writes to disk|
|badger.num_versions_to_keep|1|How many versions to keep per key|
|badger.max_table_size|64MB|Each table (or file) is at most this size|
|badger.level_size_multiplier|10|Equals SizeOf(Li+1)/SizeOf(Li)|
|badger.max_levels|7|Maximum number of levels of compaction|
|badger.value_threshold|32|If value size >= this threshold, only store value offsets in tree|
|badger.num_memtables|5|Maximum number of tables to keep in memory, before stalling|
|badger.num_level0_tables|5|Maximum number of Level 0 tables before we start compacting|
|badger.num_level0_tables_stall|10|If we hit this number of Level 0 tables, we will stall until L0 is compacted away|
|badger.level_one_size|256MB|Maximum total size for L1|
|badger.value_log_file_size|1GB|Size of single value log file|
|badger.value_log_max_entries|1000000|Max number of entries a value log file can hold (approximately). A value log file would be determined by the smaller of its file size and max entries|
|badger.num_compactors|3|Number of compaction workers to run concurrently|
|badger.do_not_compact|false|Stops LSM tree from compactions|
|badger.table_loading_mode|options.LoadToRAM|How should LSM tree be accessed|
|badger.value_log_loading_mode|options.MemoryMap|How should value log be accessed|

### RocksDB

|field|default value|description|
|-|-|-|
|rocksdb.dir|"/tmp/rocksdb"|The directory to save data|
|rocksdb.allow_concurrent_memtable_writes|true|Sets whether to allow concurrent memtable writes|
|rocksdb.allow_mmap_reads|false|Enable/Disable mmap reads for reading sst tables|
|rocksdb.allow_mmap_writes|false|Enable/Disable mmap writes for writing sst tables|
|rocksdb.arena_block_size|0(write_buffer_size / 8)|Sets the size of one block in arena memory allocation|
|rocksdb.db_write_buffer_size|0(disable)|Sets the amount of data to build up in memtables across all column families before writing to disk|
|rocksdb.hard_pending_compaction_bytes_limit|256GB|Sets the bytes threshold at which all writes are stopped if estimated bytes needed to be compaction exceed this threshold|
|rocksdb.level0_file_num_compaction_trigger|4|Sets the number of files to trigger level-0 compaction|
|rocksdb.level0_slowdown_writes_trigger|20|Sets the soft limit on number of level-0 files|
|rocksdb.level0_stop_writes_trigger|36|Sets the maximum number of level-0 files. We stop writes at this point|
|rocksdb.max_bytes_for_level_base|256MB|Sets the maximum total data size for base level|
|rocksdb.max_bytes_for_level_multiplier|10|Sets the max Bytes for level multiplier|
|rocksdb.max_total_wal_size|0(\[sum of all write_buffer_size * max_write_buffer_number\] * 4)|Sets the maximum total wal size in bytes. Once write-ahead logs exceed this size, we will start forcing the flush of column families whose memtables are backed by the oldest live WAL file (i.e. the ones that are causing all the space amplification)|
|rocksdb.memtable_huge_page_size|0|Sets the page size for huge page for arena used by the memtable|
|rocksdb.num_levels|7|Sets the number of levels for this database|
|rocksdb.use_direct_reads|false|Enable/Disable direct I/O mode (O_DIRECT) for reads|
|rocksdb.use_fsync|false|Enable/Disable fsync|
|rocksdb.write_buffer_size|64MB|Sets the amount of data to build up in memory (backed by an unsorted log on disk) before converting to a sorted on-disk file|
|rocksdb.max_write_buffer_number|2|Sets the maximum number of write buffers that are built up in memory|
|rocksdb.max_background_jobs|2|Sets maximum number of concurrent background jobs (compactions and flushes)|
|rocksdb.block_size|4KB|Sets the approximate size of user data packed per block. Note that the block size specified here corresponds opts uncompressed data. The actual size of the unit read from disk may be smaller if compression is enabled|
|rocksdb.block_size_deviation|10|Sets the block size deviation. This is used opts close a block before it reaches the configured 'block_size'. If the percentage of free space in the current block is less than this specified number and adding a new record opts the block will exceed the configured block size, then this block will be closed and the new record will be written opts the next block|
|rocksdb.cache_index_and_filter_blocks|false|Indicating if we'd put index/filter blocks to the block cache. If not specified, each "table reader" object will pre-load index/filter block during table initialization|
|rocksdb.no_block_cache|false|Specify whether block cache should be used or not|
|rocksdb.pin_l0_filter_and_index_blocks_in_cache|false|Sets cache_index_and_filter_blocks. If is true and the below is true (hash_index_allow_collision), then filter and index blocks are stored in the cache, but a reference is held in the "table reader" object so the blocks are pinned and only evicted from cache when the table reader is freed|
|rocksdb.whole_key_filtering|true|Specify if whole keys in the filter (not just prefixes) should be placed. This must generally be true for gets opts be efficient|
|rocksdb.block_restart_interval|16|Sets the number of keys between restart points for delta encoding of keys. This parameter can be changed dynamically|
|rocksdb.filter_policy|nil|Sets the filter policy opts reduce disk reads. Many applications will benefit from passing the result of NewBloomFilterPolicy() here|
|rocksdb.index_type|kBinarySearch|Sets the index type used for this table. __kBinarySearch__: A space efficient index block that is optimized for binary-search-based index. __kHashSearch__: The hash index, if enabled, will do the hash lookup when `Options.prefix_extractor` is provided. __kTwoLevelIndexSearch__: A two-level index implementation. Both levels are binary search indexes|
|rocksdb.block_align|false|Enable/Disable align data blocks on lesser of page size and block size|

### Spanner

|field|default value|description|
|-|-|-|
|spanner.db|""|Spanner Database|
|spanner.credentials|"~/.spanner/credentials.json"|Google application credentials for Spanner|

### Sqlite

|field|default value|description|
|-|-|-|
|sqlite.db|"/tmp/sqlite.db"|Database path|
|sqlite.mode|"rwc"|Open Mode: ro, rc, rwc, memory|
|sqlite.journalmode|"DELETE"|Journal mode: DELETE, TRUNCSTE, PERSIST, MEMORY, WAL, OFF|
|sqlite.cache|"Shared"|Cache: shared, private|

### Cassandra

|field|default value|description|
|-|-|-|
|cassandra.cluster|"127.0.0.1:9042"|Cassandra cluster|
|cassandra.keyspace|"test"|Keyspace|
|cassandra.connections|2|Number of connections per host|
|cassandra.username|cassandra|Username|
|cassandra.password|cassandra|Password|

### MongoDB

|field|default value|description|
|-|-|-|
|mongodb.url|"mongodb://127.0.0.1:27017"|MongoDB URI|
|mongodb.tls_skip_verify|false|Enable/disable server ca certificate verification|
|mongodb.tls_ca_file|""|Path to mongodb server ca certificate file|
|mongodb.namespace|"ycsb.ycsb"|Namespace to use|
|mongodb.authdb|"admin"|Authentication database|
|mongodb.username|N/A|Username for authentication|
|mongodb.password|N/A|Password for authentication|

### Redis
|field|default value|description|
|-|-|-|
|redis.datatype|hash|"hash", "string" or "json" ("json" requires [RedisJSON](https://redis.io/docs/stack/json/) available)|
|redis.mode|single|"single" or "cluster"|
|redis.network|tcp|"tcp" or "unix"|
|redis.addr||Redis server address(es) in "host:port" form, can be semi-colon `;` separated in cluster mode|
|redis.username||Redis server username|
|redis.password||Redis server password|
|redis.db|0|Redis server target db|
|redis.max_redirects|0|The maximum number of retries before giving up (only for cluster mode)|
|redis.read_only|false|Enables read-only commands on slave nodes (only for cluster mode)|
|redis.route_by_latency|false|Allows routing read-only commands to the closest master or slave node (only for cluster mode)|
|redis.route_randomly|false|Allows routing read-only commands to the random master or slave node (only for cluster mode)|
|redis.max_retries||Max retries before giving up connection|
|redis.min_retry_backoff|8ms|Minimum backoff between each retry|
|redis.max_retry_backoff|512ms|Maximum backoff between each retry|
|redis.dial_timeout|5s|Dial timeout for establishing new connection|
|redis.read_timeout|3s|Timeout for socket reads|
|redis.write_timeout|3s|Timeout for socket writes|
|redis.pool_size|10|Maximum number of socket connections|
|redis.min_idle_conns|0|Minimum number of idle connections|
|redis.max_idle_conns|0|Maximum number of idle connections. If <= 0, connections are not closed due to a connection's idle time.|
|redis.max_conn_age|0|Connection age at which client closes the connection|
|redis.pool_timeout|4s|Amount of time client waits for connections are busy before returning an error|
|redis.idle_timeout|5m|Amount of time after which client closes idle connections. Should be less than server timeout|
|redis.idle_check_frequency|1m|Frequency of idle checks made by idle connections reaper. Deprecated in favour of redis.max_idle_conns|
|redis.tls_ca||Path to CA file|
|redis.tls_cert||Path to cert file|
|redis.tls_key||Path to key file|
|redis.tls_insecure_skip_verify|false|Controls whether a client verifies the server's certificate chain and host name|

### BoltDB

|field|default value|description|
|-|-|-|
|bolt.path|"/tmp/boltdb"|The database file path. If the file does not exists then it will be created automatically|
|bolt.timeout|0|The amount of time to wait to obtain a file lock. When set to zero it will wait indefinitely. This option is only available on Darwin and Linux|
|bolt.no_grow_sync|false|Sets DB.NoGrowSync flag before memory mapping the file|
|bolt.read_only|false|Open the database in read-only mode|
|bolt.mmap_flags|0|Set the DB.MmapFlags flag before memory mapping the file|
|bolt.initial_mmap_size|0|The initial mmap size of the database in bytes. If <= 0, the initial map size is 0. If the size is smaller than the previous database, it takes no effect|

### etcd

|field|default value|description|
|-|-|-|
|etcd.endpoints|"localhost:2379"|The etcd endpoint(s), multiple endpoints can be passed separated by comma.|
|etcd.dial_timeout|"2s"|The dial timeout duration passed into the client config.|
|etcd.cert_file|""|When using secure etcd, this should point to the crt file.|
|etcd.key_file|""|When using secure etcd, this should point to the pem file.|
|etcd.cacert_file|""|When using secure etcd, this should point to the ca file.|
|etcd.serializable_reads|false|Whether to use serializable reads.|

### DynamoDB

|field|default value|description|
|-|-|-|
|dynamodb.tablename|"ycsb"|The database tablename|
|dynamodb.primarykey|"_key"|The table primary key fieldname|
|dynamodb.rc.units|10|Read request units throughput|
|dynamodb.wc.units|10|Write request units throughput|
|dynamodb.ensure.clean.table|true|On load mode ensure that the table is clean at the begining. In case of true and if the table previously exists it will be deleted and recreated|
|dynamodb.endpoint|""|Used endpoint for connection. If empty will use the default loaded configs|
|dynamodb.region|""|Used region for connection ( should match endpoint ). If empty will use the default loaded configs|
|dynamodb.consistent.reads|false|Reads on DynamoDB provide an eventually consistent read by default. If your benchmark/use-case requires a strongly consistent read, set this option to true|
|dynamodb.delete.after.run.stage|false|Detele the database table after the run stage|

### S3

|field|default value|description|
|-|-|-|
|s3.bucket|"ycsb"|Bucket name to use for objects|
|s3.region|"us-east-1"|AWS region (set to `auto` for LocalStack)|
|s3.endpoint|""|Custom endpoint URL (e.g. `http://localhost:4566` for LocalStack). Leave empty to use AWS public endpoint|
|s3.access_key|""|Access key (falls back to environment / profile)|
|s3.secret_key|""|Secret key|
|s3.use_path_style|false|Set `true` for LocalStack; forces path-style requests|
|s3.update_overwrite|true|Set `false` for update to perform a read-modify-write operation|
|s3.scan_keys_only|false|Set `true` to have scan return only the keys of the objects|

## TODO

- [ ] Support more measurement, like HdrHistogram
- [ ] Add tests for generators
