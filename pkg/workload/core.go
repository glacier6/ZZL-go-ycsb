// Copyright 2018 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package workload

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/magiconair/properties"
	"github.com/pingcap/go-ycsb/pkg/generator"
	"github.com/pingcap/go-ycsb/pkg/measurement"
	"github.com/pingcap/go-ycsb/pkg/prop"
	"github.com/pingcap/go-ycsb/pkg/util"
	"github.com/pingcap/go-ycsb/pkg/ycsb"
)

type contextKey string

const stateKey = contextKey("core")

type coreState struct {
	r *rand.Rand
	// fieldNames is a copy of core.fieldNames to be goroutine-local
	fieldNames []string
}

type operationType int64

const (
	read operationType = iota + 1
	update
	insert
	scan
	readModifyWrite
)

// Core is the core benchmark scenario. Represents a set of clients doing simple CRUD operations.
type core struct {
	p *properties.Properties

	table      string
	fieldCount int64
	fieldNames []string

	fieldLengthGenerator ycsb.Generator
	readAllFields        bool
	writeAllFields       bool
	dataIntegrity        bool

	keySequence                  ycsb.Generator
	operationChooser             *generator.Discrete
	keyChooser                   ycsb.Generator
	fieldChooser                 ycsb.Generator
	transactionInsertKeySequence *generator.AcknowledgedCounter
	scanLength                   ycsb.Generator
	orderedInserts               bool
	recordCount                  int64
	zeroPadding                  int64
	insertionRetryLimit          int64
	insertionRetryInterval       int64

	valuePool sync.Pool
}

func getFieldLengthGenerator(p *properties.Properties) ycsb.Generator {
	var fieldLengthGenerator ycsb.Generator
	fieldLengthDistribution := p.GetString(prop.FieldLengthDistribution, prop.FieldLengthDistributionDefault)
	fieldLength := p.GetInt64(prop.FieldLength, prop.FieldLengthDefault)
	fieldLengthHistogram := p.GetString(prop.FieldLengthHistogramFile, prop.FieldLengthHistogramFileDefault)

	switch strings.ToLower(fieldLengthDistribution) {
	case "constant":
		fieldLengthGenerator = generator.NewConstant(fieldLength)
	case "uniform":
		fieldLengthGenerator = generator.NewUniform(1, fieldLength)
	case "zipfian":
		fieldLengthGenerator = generator.NewZipfianWithRange(1, fieldLength, generator.ZipfianConstant)
	case "histogram":
		fieldLengthGenerator = generator.NewHistogramFromFile(fieldLengthHistogram)
	default:
		util.Fatalf("unknown field length distribution %s", fieldLengthDistribution)
	}

	return fieldLengthGenerator
}

func createOperationGenerator(p *properties.Properties) *generator.Discrete {
	readProportion := p.GetFloat64(prop.ReadProportion, prop.ReadProportionDefault)
	updateProportion := p.GetFloat64(prop.UpdateProportion, prop.UpdateProportionDefault)
	insertProportion := p.GetFloat64(prop.InsertProportion, prop.InsertProportionDefault)
	scanProportion := p.GetFloat64(prop.ScanProportion, prop.ScanProportionDefault)
	readModifyWriteProportion := p.GetFloat64(prop.ReadModifyWriteProportion, prop.ReadModifyWriteProportionDefault)

	operationChooser := generator.NewDiscrete()
	if readProportion > 0 {
		operationChooser.Add(readProportion, int64(read))
	}

	if updateProportion > 0 {
		operationChooser.Add(updateProportion, int64(update))
	}

	if insertProportion > 0 {
		operationChooser.Add(insertProportion, int64(insert))
	}

	if scanProportion > 0 {
		operationChooser.Add(scanProportion, int64(scan))
	}

	if readModifyWriteProportion > 0 {
		operationChooser.Add(readModifyWriteProportion, int64(readModifyWrite))
	}

	return operationChooser
}

// Load implements the Workload Load interface.
func (c *core) Load(ctx context.Context, db ycsb.DB, totalCount int64) error {
	return nil
}

// InitThread implements the Workload InitThread interface.
func (c *core) InitThread(ctx context.Context, _ int, _ int) context.Context {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	fieldNames := make([]string, len(c.fieldNames))
	copy(fieldNames, c.fieldNames)
	state := &coreState{
		r:          r,
		fieldNames: fieldNames,
	}
	return context.WithValue(ctx, stateKey, state)
}

// CleanupThread implements the Workload CleanupThread interface.
func (c *core) CleanupThread(_ context.Context) {

}

// Close implements the Workload Close interface.
func (c *core) Close() error {
	return nil
}

func (c *core) buildKeyName(keyNum int64) string {
	if !c.orderedInserts {
		keyNum = util.Hash64(keyNum)
	}

	prefix := c.p.GetString(prop.KeyPrefix, prop.KeyPrefixDefault)
	return fmt.Sprintf("%s%0[3]*[2]d", prefix, keyNum, c.zeroPadding)
}

func (c *core) buildSingleValue(state *coreState, key string) map[string][]byte {
	values := make(map[string][]byte, 1)

	r := state.r
	fieldKey := state.fieldNames[c.fieldChooser.Next(r)]

	var buf []byte
	if c.dataIntegrity {
		buf = c.buildDeterministicValue(state, key, fieldKey)
	} else {
		buf = c.buildRandomValue(state)
	}

	values[fieldKey] = buf

	return values
}

func (c *core) buildValues(state *coreState, key string) map[string][]byte {
	values := make(map[string][]byte, c.fieldCount)

	for _, fieldKey := range state.fieldNames {
		var buf []byte
		if c.dataIntegrity {
			buf = c.buildDeterministicValue(state, key, fieldKey)
		} else {
			buf = c.buildRandomValue(state)
		}

		values[fieldKey] = buf
	}
	return values
}

func (c *core) getValueBuffer(size int) []byte {
	buf := c.valuePool.Get().([]byte)
	if cap(buf) >= size {
		return buf[0:size]
	}

	return make([]byte, size)
}

func (c *core) putValues(values map[string][]byte) {
	for _, value := range values {
		c.valuePool.Put(value)
	}
}

func (c *core) buildRandomValue(state *coreState) []byte {
	// TODO: use pool for the buffer
	r := state.r
	buf := c.getValueBuffer(int(c.fieldLengthGenerator.Next(r)))
	util.RandBytes(r, buf)
	return buf
}

func (c *core) buildDeterministicValue(state *coreState, key string, fieldKey string) []byte {
	// TODO: use pool for the buffer
	r := state.r
	size := c.fieldLengthGenerator.Next(r)
	buf := c.getValueBuffer(int(size + 21))
	b := bytes.NewBuffer(buf[0:0])
	b.WriteString(key)
	b.WriteByte(':')
	b.WriteString(strings.ToLower(fieldKey))
	for int64(b.Len()) < size {
		b.WriteByte(':')
		n := util.BytesHash64(b.Bytes())
		b.WriteString(strconv.FormatUint(uint64(n), 10))
	}
	b.Truncate(int(size))
	return b.Bytes()
}

func (c *core) verifyRow(state *coreState, key string, values map[string][]byte) {
	if len(values) == 0 {
		// null data here, need panic?
		return
	}

	for fieldKey, value := range values {
		expected := c.buildDeterministicValue(state, key, fieldKey)
		if !bytes.Equal(expected, value) {
			util.Fatalf("unexpected deterministic value, expect %q, but got %q", expected, value)
		}
	}
}

// DoInsert implements the Workload DoInsert interface.
func (c *core) DoInsert(ctx context.Context, db ycsb.DB) error {
	state := ctx.Value(stateKey).(*coreState)
	r := state.r
	keyNum := c.keySequence.Next(r)
	dbKey := c.buildKeyName(keyNum)
	values := c.buildValues(state, dbKey)
	defer c.putValues(values)

	numOfRetries := int64(0)

	var err error
	for {
		err = db.Insert(ctx, c.table, dbKey, values)
		if err != nil {
			break
		}

		select {
		case <-ctx.Done():
			if ctx.Err() == context.Canceled {
				return nil
			}
		default:
		}

		// Retry if configured. Without retrying, the load process will fail
		// even if one single insertion fails. User can optionally configure
		// an insertion retry limit (default is 0) to enable retry.
		numOfRetries++
		if numOfRetries > c.insertionRetryLimit {
			break
		}

		// Sleep for a random time betweensz [0.8, 1.2)*insertionRetryInterval
		sleepTimeMs := float64((c.insertionRetryInterval * 1000)) * (0.8 + 0.4*r.Float64())

		time.Sleep(time.Duration(sleepTimeMs) * time.Millisecond)
	}

	return err
}

// DoBatchInsert implements the Workload DoBatchInsert interface.
func (c *core) DoBatchInsert(ctx context.Context, batchSize int, db ycsb.DB) error {
	batchDB, ok := db.(ycsb.BatchDB)
	if !ok {
		return fmt.Errorf("the %T does't implement the batchDB interface", db)
	}
	state := ctx.Value(stateKey).(*coreState)
	r := state.r
	var keys []string
	var values []map[string][]byte
	for i := 0; i < batchSize; i++ {
		keyNum := c.keySequence.Next(r)
		dbKey := c.buildKeyName(keyNum)
		keys = append(keys, dbKey)
		values = append(values, c.buildValues(state, dbKey))
	}
	defer func() {
		for _, value := range values {
			c.putValues(value)
		}
	}()

	numOfRetries := int64(0)
	var err error
	for {
		err = batchDB.BatchInsert(ctx, c.table, keys, values)
		if err != nil {
			break
		}

		select {
		case <-ctx.Done():
			if ctx.Err() == context.Canceled {
				return nil
			}
		default:
		}

		// Retry if configured. Without retrying, the load process will fail
		// even if one single insertion fails. User can optionally configure
		// an insertion retry limit (default is 0) to enable retry.
		numOfRetries++
		if numOfRetries > c.insertionRetryLimit {
			break
		}

		// Sleep for a random time betweensz [0.8, 1.2)*insertionRetryInterval
		sleepTimeMs := float64((c.insertionRetryInterval * 1000)) * (0.8 + 0.4*r.Float64())

		time.Sleep(time.Duration(sleepTimeMs) * time.Millisecond)
	}
	return err
}

// DoTransaction implements the Workload DoTransaction interface.
func (c *core) DoTransaction(ctx context.Context, db ycsb.DB) error {
	state := ctx.Value(stateKey).(*coreState)
	r := state.r

	// NOTE:2026033000 扔操作类型骰子
	// zzlHACK: 改为先决定key,然后再决定操作的顺序
	keyNum := int64(-1) // 决定key
	var operation operationType
	// 使用 Go 的类型断言：判断当前使用的 keyGenerator 是不是我们手写的 MixGraph？
	// 如果是，就去查它专属的读写比例表！
	if mg, ok := c.keyChooser.(*generator.PrefixMixGraphGenerator); ok {
		keyNum = c.nextKeyNum(state)
		operation = operationType(mg.ChooseOperationForKey(keyNum, state.r))
	} else {
		// 如果用户没用 MixGraph（比如用的 zipfian），就退化回 YCSB 默认的全局轮盘赌
		operation = operationType(c.operationChooser.Next(r)) // NOTE:原版的核心操作,轮盘赌得到目标操作类型
	}

	switch operation {
	case read:
		return c.doTransactionRead(ctx, db, state, keyNum)
	case update:
		return c.doTransactionUpdate(ctx, db, state, keyNum)
	case insert:
		return c.doTransactionInsert(ctx, db, state)
	case scan:
		return c.doTransactionScan(ctx, db, state, keyNum)
	default:
		return c.doTransactionReadModifyWrite(ctx, db, state, keyNum)
	}
	// zzlHACK:END
}

// DoBatchTransaction implements the Workload DoBatchTransaction interface
func (c *core) DoBatchTransaction(ctx context.Context, batchSize int, db ycsb.DB) error {
	batchDB, ok := db.(ycsb.BatchDB)
	if !ok {
		return fmt.Errorf("the %T does't implement the batchDB interface", db)
	}
	state := ctx.Value(stateKey).(*coreState)
	r := state.r

	operation := operationType(c.operationChooser.Next(r))
	switch operation {
	case read:
		return c.doBatchTransactionRead(ctx, batchSize, batchDB, state)
	case insert:
		return c.doBatchTransactionInsert(ctx, batchSize, batchDB, state)
	case update:
		return c.doBatchTransactionUpdate(ctx, batchSize, batchDB, state)
	case scan:
		panic("The batch mode don't support the scan operation")
	default:
		return nil
	}
}

func (c *core) nextKeyNum(state *coreState) int64 {
	r := state.r
	keyNum := int64(0)
	if _, ok := c.keyChooser.(*generator.Exponential); ok {
		keyNum = -1
		for keyNum < 0 {
			keyNum = c.transactionInsertKeySequence.Last() - c.keyChooser.Next(r)
		}
	} else {
		keyNum = c.keyChooser.Next(r) // 得到对应访问模式的key
	}
	return keyNum
}

func (c *core) doTransactionRead(ctx context.Context, db ycsb.DB, state *coreState, keyNum int64) error {
	r := state.r
	// zzlHACK:
	if keyNum == int64(-1) {
		keyNum = c.nextKeyNum(state) // NOTE:核心操作,得到key的那个数字,内部会调用对应生成器的next函数
	}
	// zzlHACK:END
	keyName := c.buildKeyName(keyNum) // NOTE:核心操作,拼上前缀,即得到最终的在badger内的那个key

	var fields []string
	if !c.readAllFields {
		fieldName := state.fieldNames[c.fieldChooser.Next(r)]
		fields = append(fields, fieldName)
	} else {
		fields = state.fieldNames
	}

	values, err := db.Read(ctx, c.table, keyName, fields)
	if err != nil {
		return err
	}

	if c.dataIntegrity {
		c.verifyRow(state, keyName, values)
	}

	return nil
}

func (c *core) doTransactionReadModifyWrite(ctx context.Context, db ycsb.DB, state *coreState, keyNum int64) error {
	start := time.Now()
	defer func() {
		measurement.Measure("READ_MODIFY_WRITE", start, time.Now().Sub(start))
	}()

	r := state.r
	// zzlHACK:
	if keyNum == int64(-1) {
		keyNum = c.nextKeyNum(state) // NOTE:核心操作,得到key的那个数字,内部会调用对应生成器的next函数
	}
	// zzlHACK:END
	keyName := c.buildKeyName(keyNum)

	var fields []string
	if !c.readAllFields {
		fieldName := state.fieldNames[c.fieldChooser.Next(r)]
		fields = append(fields, fieldName)
	} else {
		fields = state.fieldNames
	}

	var values map[string][]byte
	if c.writeAllFields {
		values = c.buildValues(state, keyName)
	} else {
		values = c.buildSingleValue(state, keyName)
	}
	defer c.putValues(values)

	readValues, err := db.Read(ctx, c.table, keyName, fields)
	if err != nil {
		return err
	}

	if err := db.Update(ctx, c.table, keyName, values); err != nil {
		return err
	}

	if c.dataIntegrity {
		c.verifyRow(state, keyName, readValues)
	}

	return nil
}

func (c *core) doTransactionInsert(ctx context.Context, db ycsb.DB, state *coreState) error {
	r := state.r
	keyNum := c.transactionInsertKeySequence.Next(r)
	defer c.transactionInsertKeySequence.Acknowledge(keyNum)
	dbKey := c.buildKeyName(keyNum)
	values := c.buildValues(state, dbKey)
	defer c.putValues(values)

	return db.Insert(ctx, c.table, dbKey, values)
}

func (c *core) doTransactionScan(ctx context.Context, db ycsb.DB, state *coreState, keyNum int64) error {
	r := state.r
	// zzlHACK:
	if keyNum == int64(-1) {
		keyNum = c.nextKeyNum(state) // NOTE:核心操作,得到key的那个数字,内部会调用对应生成器的next函数
	}
	// zzlHACK:END
	startKeyName := c.buildKeyName(keyNum)

	scanLen := c.scanLength.Next(r)

	var fields []string
	if !c.readAllFields {
		fieldName := state.fieldNames[c.fieldChooser.Next(r)]
		fields = append(fields, fieldName)
	} else {
		fields = state.fieldNames
	}

	_, err := db.Scan(ctx, c.table, startKeyName, int(scanLen), fields)

	return err
}

func (c *core) doTransactionUpdate(ctx context.Context, db ycsb.DB, state *coreState, keyNum int64) error {
	// zzlHACK:
	if keyNum == int64(-1) {
		keyNum = c.nextKeyNum(state) // NOTE:核心操作,得到key的那个数字,内部会调用对应生成器的next函数
	}
	// zzlHACK:END
	keyName := c.buildKeyName(keyNum)

	var values map[string][]byte
	if c.writeAllFields {
		values = c.buildValues(state, keyName)
	} else {
		values = c.buildSingleValue(state, keyName)
	}

	defer c.putValues(values)

	return db.Update(ctx, c.table, keyName, values)
}

func (c *core) doBatchTransactionRead(ctx context.Context, batchSize int, db ycsb.BatchDB, state *coreState) error {
	r := state.r
	var fields []string

	if !c.readAllFields {
		fieldName := state.fieldNames[c.fieldChooser.Next(r)]
		fields = append(fields, fieldName)
	} else {
		fields = state.fieldNames
	}

	keys := make([]string, batchSize)
	for i := 0; i < batchSize; i++ {
		keys[i] = c.buildKeyName(c.nextKeyNum(state))
	}

	_, err := db.BatchRead(ctx, c.table, keys, fields)
	if err != nil {
		return err
	}

	// TODO should we verify the result?
	return nil
}

func (c *core) doBatchTransactionInsert(ctx context.Context, batchSize int, db ycsb.BatchDB, state *coreState) error {
	r := state.r
	keys := make([]string, batchSize)
	values := make([]map[string][]byte, batchSize)
	for i := 0; i < batchSize; i++ {
		keyNum := c.transactionInsertKeySequence.Next(r)
		keyName := c.buildKeyName(keyNum)
		keys[i] = keyName
		if c.writeAllFields {
			values[i] = c.buildValues(state, keyName)
		} else {
			values[i] = c.buildSingleValue(state, keyName)
		}
		c.transactionInsertKeySequence.Acknowledge(keyNum)
	}

	defer func() {
		for _, value := range values {
			c.putValues(value)
		}
	}()

	return db.BatchInsert(ctx, c.table, keys, values)
}

func (c *core) doBatchTransactionUpdate(ctx context.Context, batchSize int, db ycsb.BatchDB, state *coreState) error {
	keys := make([]string, batchSize)
	values := make([]map[string][]byte, batchSize)
	for i := 0; i < batchSize; i++ {
		keyNum := c.nextKeyNum(state)
		keyName := c.buildKeyName(keyNum)
		keys[i] = keyName
		if c.writeAllFields {
			values[i] = c.buildValues(state, keyName)
		} else {
			values[i] = c.buildSingleValue(state, keyName)
		}
	}

	defer func() {
		for _, value := range values {
			c.putValues(value)
		}
	}()

	return db.BatchUpdate(ctx, c.table, keys, values)
}

// CoreCreator creates the Core workload.
type coreCreator struct {
}

// Create implements the WorkloadCreator Create interface.
func (coreCreator) Create(p *properties.Properties) (ycsb.Workload, error) {
	c := new(core)
	c.p = p
	c.table = p.GetString(prop.TableName, prop.TableNameDefault)
	c.fieldCount = p.GetInt64(prop.FieldCount, prop.FieldCountDefault)
	c.fieldNames = make([]string, c.fieldCount)
	for i := int64(0); i < c.fieldCount; i++ {
		c.fieldNames[i] = fmt.Sprintf("field%d", i)
	}
	c.fieldLengthGenerator = getFieldLengthGenerator(p)
	c.recordCount = p.GetInt64(prop.RecordCount, prop.RecordCountDefault)
	if c.recordCount == 0 {
		c.recordCount = int64(math.MaxInt32)
	}

	requestDistrib := p.GetString(prop.RequestDistribution, prop.RequestDistributionDefault)
	minScanLength := p.GetInt64(prop.MinScanLength, prop.MinScanLengthDefault)
	maxScanLength := p.GetInt64(prop.MaxScanLength, prop.MaxScanLengthDefault)
	scanLengthDistrib := p.GetString(prop.ScanLengthDistribution, prop.ScanLengthDistributionDefault)

	insertStart := p.GetInt64(prop.InsertStart, prop.InsertStartDefault)
	insertCount := p.GetInt64(prop.InsertCount, c.recordCount-insertStart)
	if c.recordCount < insertStart+insertCount {
		util.Fatalf("record count %d must be bigger than insert start %d + count %d",
			c.recordCount, insertStart, insertCount)
	}
	c.zeroPadding = p.GetInt64(prop.ZeroPadding, prop.ZeroPaddingDefault)
	c.readAllFields = p.GetBool(prop.ReadAllFields, prop.ReadALlFieldsDefault)
	c.writeAllFields = p.GetBool(prop.WriteAllFields, prop.WriteAllFieldsDefault)
	c.dataIntegrity = p.GetBool(prop.DataIntegrity, prop.DataIntegrityDefault)
	fieldLengthDistribution := p.GetString(prop.FieldLengthDistribution, prop.FieldLengthDistributionDefault)
	if c.dataIntegrity && fieldLengthDistribution != "constant" {
		util.Fatal("must have constant field size to check data integrity")
	}

	if p.GetString(prop.InsertOrder, prop.InsertOrderDefault) == "hashed" {
		c.orderedInserts = false
	} else {
		c.orderedInserts = true
	}

	c.keySequence = generator.NewCounter(insertStart)
	c.operationChooser = createOperationGenerator(p)
	var keyrangeLowerBound int64 = insertStart
	var keyrangeUpperBound int64 = insertStart + insertCount - 1

	c.transactionInsertKeySequence = generator.NewAcknowledgedCounter(c.recordCount)
	switch requestDistrib {
	case "uniform":
		c.keyChooser = generator.NewUniform(keyrangeLowerBound, keyrangeUpperBound)
	case "sequential":
		c.keyChooser = generator.NewSequential(keyrangeLowerBound, keyrangeUpperBound)
	case "zipfian":
		insertProportion := p.GetFloat64(prop.InsertProportion, prop.InsertProportionDefault)
		opCount := p.GetInt64(prop.OperationCount, 0)
		expectedNewKeys := int64(float64(opCount) * insertProportion * 2.0)
		keyrangeUpperBound = insertStart + insertCount + expectedNewKeys
		c.keyChooser = generator.NewScrambledZipfian(keyrangeLowerBound, keyrangeUpperBound, generator.ZipfianConstant)
	case "latest":
		c.keyChooser = generator.NewSkewedLatest(c.transactionInsertKeySequence)
	case "hotspot":
		hotsetFraction := p.GetFloat64(prop.HotspotDataFraction, prop.HotspotDataFractionDefault)
		hotopnFraction := p.GetFloat64(prop.HotspotOpnFraction, prop.HotspotOpnFractionDefault)
		c.keyChooser = generator.NewHotspot(keyrangeLowerBound, keyrangeUpperBound, hotsetFraction, hotopnFraction)
	case "exponential":
		percentile := p.GetFloat64(prop.ExponentialPercentile, prop.ExponentialPercentileDefault)
		frac := p.GetFloat64(prop.ExponentialFrac, prop.ExponentialFracDefault)
		c.keyChooser = generator.NewExponential(percentile, float64(c.recordCount)*frac)
	// zzlHACK:
	case "mixgraph":
		numRegions := p.GetInt64(prop.MixGraphNumRegions, prop.MixGraphNumRegionsDefault)
		expA := p.GetFloat64(prop.MixGraphExpA, prop.MixGraphExpADefault)
		expB := p.GetFloat64(prop.MixGraphExpB, prop.MixGraphExpBDefault)
		expC := p.GetFloat64(prop.MixGraphExpC, prop.MixGraphExpCDefault)
		expD := p.GetFloat64(prop.MixGraphExpD, prop.MixGraphExpDDefault)
		keyDistA := p.GetFloat64(prop.MixGraphKeyDistA, prop.MixGraphKeyDistADefault)
		keyDistB := p.GetFloat64(prop.MixGraphKeyDistB, prop.MixGraphKeyDistBDefault)

		c.keyChooser = generator.NewPrefixMixGraphGenerator(
			c.recordCount, numRegions,
			expA, expB, expC, expD,
			keyDistA, keyDistB,
		)
		// 👇 新增：在这里劫持 Value 长度生成器 👇
		if p.GetBool("mixgraph.gpd_value", false) {
			valueK := p.GetFloat64("mixgraph.value_k", 0.2615)
			valueSigma := p.GetFloat64("mixgraph.value_sigma", 25.45)
			valueMu := p.GetFloat64("mixgraph.value_mu", 0.0)
			c.fieldLengthGenerator = generator.NewGPDGenerator(valueK, valueSigma, valueMu)
			fmt.Printf("🚀 [CoreWorkload] GPD 动态 Value 开关【已开启】! (k=%.4f, sigma=%.2f)\n", valueK, valueSigma)
		} else {
			// 留一个提示日志，方便你压测时确认状态
			fmt.Println("⚪ [CoreWorkload] GPD 动态 Value 开关【未开启】，将使用原版固定 fieldlength。")
		}

	// zzlHACK:END
	default:
		util.Fatalf("unknown request distribution %s", requestDistrib)
	}
	fmt.Println(fmt.Sprintf("Using request distribution '%s' a keyrange of [%d %d]", requestDistrib, keyrangeLowerBound, keyrangeUpperBound))

	c.fieldChooser = generator.NewUniform(0, c.fieldCount-1)
	switch scanLengthDistrib {
	case "uniform":
		c.scanLength = generator.NewUniform(minScanLength, maxScanLength)
	case "zipfian":
		c.scanLength = generator.NewZipfianWithRange(minScanLength, maxScanLength, generator.ZipfianConstant)
	default:
		util.Fatalf("distribution %s not allowed for scan length", scanLengthDistrib)
	}

	c.insertionRetryLimit = p.GetInt64(prop.InsertionRetryLimit, prop.InsertionRetryLimitDefault)
	c.insertionRetryInterval = p.GetInt64(prop.InsertionRetryInterval, prop.InsertionRetryIntervalDefault)

	fieldLength := p.GetInt64(prop.FieldLength, prop.FieldLengthDefault)
	c.valuePool = sync.Pool{
		New: func() interface{} {
			return make([]byte, fieldLength)
		},
	}

	return c, nil
}

func init() {
	ycsb.RegisterWorkloadCreator("core", coreCreator{})
	ycsb.RegisterWorkloadCreator("site.ycsb.workloads.CoreWorkload", coreCreator{})
}
