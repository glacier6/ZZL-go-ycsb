package generator

import (
	"math"
	"math/rand"
	"sort"
)

// PrefixMixGraphGenerator 实现 RocksDB MixGraph 的前缀空间局部性模型
type PrefixMixGraphGenerator struct {
	numKeys    int64
	numRegions int64
	regionSize int64
	regionCDF  []float64 // 用于第一重映射（选区间）的累积分布
	keyDistA   float64   // 用于第二重映射（选 Key）的参数 a
	keyDistB   float64   // 用于第二重映射（选 Key）的参数 b
	lastVal    int64
}

// NewPrefixMixGraphGenerator 初始化双重映射生成器
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
	cdf := make([]float64, numRegions)
	var cumulative float64 = 0
	for i := int64(0); i < numRegions; i++ {
		cumulative += weights[i] / totalWeight
		cdf[i] = cumulative
	}
	cdf[numRegions-1] = 1.0

	return &PrefixMixGraphGenerator{
		numKeys:    numKeys,
		numRegions: numRegions,
		regionSize: regionSize,
		regionCDF:  cdf,
		keyDistA:   keyDistA,
		keyDistB:   keyDistB,
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
	// 第一重映射：掷飞镖，锁定热点区间 (前缀)
	// ==========================================
	u1 := r.Float64()
	targetRegion := int64(sort.SearchFloat64s(m.regionCDF, u1))
	if targetRegion >= m.numRegions {
		targetRegion = m.numRegions - 1
	}
	regionBase := targetRegion * m.regionSize

	// ==========================================
	// 第二重映射：锁定区间内的倾斜 Key (偏移量)
	// ==========================================
	u2 := r.Float64()

	// 1. 获取倾斜特征值 (高概率会重复，比如总是算出 0 或 1)
	skewedFeature := m.powerCdfInversion(u2)

	// 2. 【核心修复】：将倾斜特征与当前的区间号 (targetRegion) 混合！
	// 这样，即使在不同区间抽到了相同的热点特征，算出来的 Hash 也是完全不同的。
	// 我们用一个简单的移位和异或组合来混合它们：
	mixedHash := uint64(skewedFeature) ^ (uint64(targetRegion) << 16) ^ 0x9e3779b97f4a7c15

	// 3. 将混合后的哈希值再进行一次 LCG 打散，确保伪随机性
	lcgValue := mixedHash*6364136223846793005 + 1442695040888963407

	// 4. 折叠回区间大小
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
