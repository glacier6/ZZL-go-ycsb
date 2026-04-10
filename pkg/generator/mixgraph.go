package generator

import (
	"math"
	"math/rand"
	"sort"
)

// PrefixMixGraphGenerator 实现 RocksDB MixGraph 的前缀空间局部性模型 (含洗牌逻辑)
type PrefixMixGraphGenerator struct {
	numKeys    int64
	numRegions int64
	regionSize int64
	// regionCDF 不再直接对应物理前缀 0, 1, 2...
	// 而是对应洗牌后的“热度桶”累积概率
	regionCDF []float64

	// regionMap 记录：第 i 个“热度桶”，被洗牌映射到了哪个实际的“物理前缀 ID”
	// 例如：最热的桶 (i=0) 可能被映射到了物理前缀 47。
	regionMap []int64

	keyDistA float64
	keyDistB float64
	lastVal  int64

	// 每个物理区间拥有自己独立的 Discrete 生成器
	regionChoosers []*Discrete
}

// NewPrefixMixGraphGenerator 初始化双重映射生成器 (含洗牌)
// NOTE:第一步映射看expX的参数,控制区间热度
// expA和expB控制超级头部.
// 	-- expA决定最热区间拿到多大的初始权重(起点高度)
// 	-- expB是区间间热度跌落的速度(衰减斜率)
// expC和expD控制尾部,为排名靠后的大量冷数据配比访问权重.
// 	-- expC可以理解为当热点流量褪去后，冷数据的访问概率从多高的起点开始平摊.
//  -- expD则是尾部的衰减斜率,越接近0,则尾部约平坦
// NOTE:第二步映射看keyDistX的参数,控制区间内各个key的热度曲线
// keyDistA和keyDistB控制区间内各个key的访问曲线
//	-- keyDistA只是一个缩放放大镜,主要是配合底层的类型转换和哈希扰动做放大使用，通常设置为 1.0 或某个常数即可，它不改变分布的“形状”，只改变数值的绝对尺度。
//  -- keyDistB是第二阶段的核心参数,如果为1则全部key访问量均等,如果为0.5则会有极大的头部,几乎90%会到同一个key上
// 下面是默认值
// MixGraphExpADefault       = float64(0.5)
// MixGraphExpBDefault       = float64(-0.1)
// MixGraphExpCDefault       = float64(0.5)
// MixGraphExpDDefault       = float64(-0.01)
// MixGraphKeyDistADefault   = float64(1.0)
// MixGraphKeyDistBDefault   = float64(1)

func NewPrefixMixGraphGenerator(numKeys, numRegions int64, expA, expB, expC, expD, keyDistA, keyDistB float64) *PrefixMixGraphGenerator {
	if numRegions <= 0 {
		numRegions = 100 // 默认保底 100 个区间
	}
	regionSize := numKeys / numRegions
	if regionSize == 0 {
		regionSize = 1
	}

	// 1. 根据双项指数分布 f(x) = a*e^(bx) + c*e^(dx) 计算区间热度权重
	weights := make([]float64, numRegions)
	var totalWeight float64 = 0

	for i := int64(0); i < numRegions; i++ {
		x := float64(i)
		w := expA*math.Exp(expB*x) + expC*math.Exp(expD*x)
		if w < 0 {
			w = 0
		}
		weights[i] = w
		totalWeight += w
	}

	// 2. 构建 CDF (累积分布函数) 用于 O(log N) 随机投掷
	// 这里的 CDF 是按照热度从高到低排序的概率桶
	cdf := make([]float64, numRegions)
	var cumulative float64 = 0
	for i := int64(0); i < numRegions; i++ {
		cumulative += weights[i] / totalWeight
		cdf[i] = cumulative
	}
	cdf[numRegions-1] = 1.0

	// ==========================================
	// 3. 【新增：神来之笔】洗牌逻辑 (Shuffle)
	// ==========================================
	// 我们初始化一个映射表，起初：热度桶 0 对应物理前缀 0，桶 1 对应物理前缀 1...
	regionMap := make([]int64, numRegions)
	for i := int64(0); i < numRegions; i++ {
		regionMap[i] = i
	}

	// 完美复刻 RocksDB 的 Random64 洗牌
	// 使用固定的种子 (例如总区间数或某个常数)，确保每次压测跑出的热点分布形态一致，
	// 这对于对比测试不同的存储引擎或 compaction 策略至关重要。
	shuffleRand := rand.New(rand.NewSource(numRegions * 997)) // 997 是个任意的素数种子

	for i := int64(0); i < numRegions; i++ {
		// 生成一个 0 到 numRegions-1 的随机位置
		pos := shuffleRand.Int63n(numRegions)
		// 交换这两个物理前缀的映射
		regionMap[i], regionMap[pos] = regionMap[pos], regionMap[i]
	}

	// 【新增】：为每个物理区间构建专属的操作轮盘
	choosers := make([]*Discrete, numRegions)

	for i := int64(0); i < numRegions; i++ {
		chooser := NewDiscrete()
		// 计算当前区间在整个 Key 空间中的位置百分比 (0.0 ~ 1.0)
		pos := float64(i) / float64(numRegions)
		// ----------------------------------------------------
		// NOTE:2026033001 这里可以调配各个区间的读写比例
		// 你可以根据区间 i 的特性（热区还是冷区），配置完全不同的比例。
		// ----------------------------------------------------
		// 1=read, 2=update, 3=insert, 4=scan, 5=readModifyWrite
		// 注意 update 会先get再set,会计入两次!
		if pos <= 0.10 {
			// 🔥 [Top 10% 核心热区] - 对应 i == 0, 1 等
			// 模拟：热门话题、活跃用户 Session
			// 策略：高频更新，制造海量过期版本，压榨 Compaction 性能
			chooser.Add(0.1, 1) // 50% Read
			chooser.Add(0.9, 2) // 50% Update

		} else if pos > 0.10 && pos <= 0.30 {
			// ⛅ [10% - 30% 温和区] - 相当于之前的 i >= 10 && i <= 20
			// 模拟：一般社交动态、近期 Timeline 翻阅
			// 策略：读为主，加入少量 Scan 以增加 LSM-Tree 检索压力
			chooser.Add(0.10, 1) // 80% Read
			chooser.Add(0.9, 2)  // 15% Update
			// chooser.Add(0.05, 4) // 5%  Scan

		} else {
			// ❄️ [30% - 100% 广阔冷区]
			// 模拟：历史存档、僵尸账号
			// 策略：极高比例的纯读，Update 极少
			// 战术意义：如果这部分的 SSTable 被频繁 Compaction，说明写放大失控
			chooser.Add(0.1, 1) // 95% Read
			chooser.Add(0.9, 2) // 5%  Update
		}
		// 【关键修复】：把它存放在它洗牌后对应的【物理前缀索引】下！
		physicalRegionID := regionMap[i]
		choosers[physicalRegionID] = chooser
	}

	return &PrefixMixGraphGenerator{
		numKeys:        numKeys,
		numRegions:     numRegions,
		regionSize:     regionSize,
		regionCDF:      cdf,
		regionMap:      regionMap, // 保存洗牌后的映射表
		keyDistA:       keyDistA,
		keyDistB:       keyDistB,
		regionChoosers: choosers,
	}
}

// powerCdfInversion 幂律分布逆变换
func (m *PrefixMixGraphGenerator) powerCdfInversion(u float64) int64 {
	if u < 0.00000001 {
		u = 0.00000001
	}
	// x = a * exp(ln(u)/b)
	val := m.keyDistA * math.Exp(math.Log(u)/m.keyDistB)
	return int64(val * 10000000) // 放大成整数种子
}

// Next 生成下一个具有前缀聚集效应的 Key ID
func (m *PrefixMixGraphGenerator) Next(r *rand.Rand) int64 {
	// ==========================================
	// 第一重映射：掷飞镖，锁定“热度桶”
	// ==========================================
	u1 := r.Float64()
	hotnessBucket := int64(sort.SearchFloat64s(m.regionCDF, u1))
	if hotnessBucket >= m.numRegions {
		hotnessBucket = m.numRegions - 1
	}

	// ==========================================
	// 【新增：洗牌映射】通过查表，将“热度桶”转换为真实的“物理前缀 ID”
	// ==========================================
	physicalRegion := m.regionMap[hotnessBucket]

	// 计算该物理区间在全局 Key 空间中的起点
	regionBase := physicalRegion * m.regionSize

	// ==========================================
	// 第二重映射：锁定物理区间内的倾斜 Key (偏移量)
	// ==========================================
	u2 := r.Float64()

	// 1. 获取倾斜特征值
	skewedFeature := m.powerCdfInversion(u2)

	// 2. 带盐哈希：将倾斜特征与当前的【物理前缀 ID】混合！
	// 注意这里混入的是 physicalRegion，确保不同的物理区间算出不同的偏移
	mixedHash := uint64(skewedFeature) ^ (uint64(physicalRegion) << 16) ^ 0x9e3779b97f4a7c15

	// 3. LCG 打散
	lcgValue := mixedHash*6364136223846793005 + 1442695040888963407

	// 4. 取模折叠
	offset := int64(lcgValue % uint64(m.regionSize))

	// ==========================================
	// 计算最终物理 KeyID
	// ==========================================
	finalKey := regionBase + offset
	if finalKey >= m.numKeys {
		finalKey = m.numKeys - 1
	}

	m.lastVal = finalKey
	return finalKey
}

func (m *PrefixMixGraphGenerator) Last() int64 {
	return m.lastVal
}

// 【新增】：根据 Key 获取其所在区间的操作指令
func (m *PrefixMixGraphGenerator) ChooseOperationForKey(keyNum int64, r *rand.Rand) int64 {
	physicalRegion := keyNum / m.regionSize
	if physicalRegion >= m.numRegions {
		physicalRegion = m.numRegions - 1
	}

	// 找到该区间的专属轮盘，掷骰子，返回指令 (READ, UPDATE, SCAN...)
	return m.regionChoosers[physicalRegion].Next(r)
}
