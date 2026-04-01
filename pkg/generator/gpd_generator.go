package generator

import (
	"math"
	"math/rand"
	"sync/atomic"
)

// GPDGenerator 实现了基于广义帕累托分布 (Generalized Pareto Distribution) 的长度生成器
type GPDGenerator struct {
	k       float64 // 形状参数 (Shape)
	sigma   float64 // 尺度参数 (Scale)
	mu      float64 // 位置参数 (Location/Base)
	lastVal int64
}

// NewGPDGenerator 初始化一个 GPD 生成器
// 对应 FAST '20 参数: k=0.2615, sigma=25.45
func NewGPDGenerator(k, sigma, mu float64) *GPDGenerator {
	return &GPDGenerator{
		k:     k,
		sigma: sigma,
		mu:    mu,
	}
}

// Next 生成下一个符合 GPD 分布的 Value 长度
func (g *GPDGenerator) Next(r *rand.Rand) int64 {
	// 生成 (0, 1) 之间的均匀分布随机数，避免 0 和 1 导致除零或无穷大
	u := r.Float64()
	for u == 0.0 || u == 1.0 {
		u = r.Float64()
	}

	var val float64
	if math.Abs(g.k) < 1e-9 {
		// 当 k 趋近于 0 时，GPD 退化为指数分布: x = mu - sigma * ln(1-u)
		val = g.mu - g.sigma*math.Log(1-u)
	} else {
		// 广义帕累托分布逆变换公式: x = mu + (sigma / k) * ((1-u)^(-k) - 1)
		val = g.mu + (g.sigma/g.k)*(math.Pow(1-u, -g.k)-1)
	}
	valInt := int64(val)
	// 保底机制：Value 长度不能为负数或 0，最少 1 个字节
	if valInt <= 0 {
		valInt = 1
	}
	// 👇 使用原子操作安全地更新 lastVal
	atomic.StoreInt64(&g.lastVal, valInt)
	return valInt
}

func (g *GPDGenerator) Last() int64 {
	// 👇 使用原子操作安全地读取
	return atomic.LoadInt64(&g.lastVal)
}
