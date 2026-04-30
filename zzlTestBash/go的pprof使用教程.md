1.先引用以及main函数中初始化
  import(
    _ "net/http/pprof"
  )
  main(){
    addr := globalProps.GetString(prop.DebugPprof, prop.DebugPprofDefault)
    go func() {
      http.ListenAndServe(addr, nil)
    }()
  }
2.运行目标程序
3.运行pprof 
  抓取CPU的指令
  go tool pprof -http=:8080 http://localhost:6060/debug/pprof/profile?seconds=30
  注意6060和main函数中的一致，而8080是网页版性能分析的地址,20是抓取多长时间
  PS：如果不加-http参数，那么就需要用命令行查看，体验感不好

  抓取内存指令
  go tool pprof -http=:8080 http://localhost:6060/debug/pprof/heap
  go tool pprof -http=:8080 http://localhost:6060/debug/pprof/heap?seconds=30
  PS：加上了 ?seconds=20 这个参数后，pprof 的行为是“先拍一张照，等20秒，再拍第二张照，然后把两张照片相减”。
  抓内存的左上角的SAMPLE的四个值分别表示：
  inuse_space (当前存活的内存空间，默认选项，或者20秒内的内存“净增长” / 净吞吐量)
  inuse_objects (当前存活的对象个数，或者20秒内对象个数的“净增长”)
  alloc_space (历史累计分配的内存空间，或者20秒内开口要了多少内存 NOTE:重要)
  alloc_objects (历史累计分配的对象个数，或者20秒内 new 出来的总对象数)

4.打开网页版pprof，然后一般主要用两个，一个是函数调用图，一个火焰图（下面是抓CPU的怎么看，而抓内存的看的逻辑都一样，只不过从执行时间变成了内存空间大小）
  函数调用图（VIEW-Graph）：
    （1）每一个方块代表一个函数。方块的颜色越红、面积越大，说明这个函数占用的 CPU 时间越多。方块内第一行时间是这个函数自身运行花费的时间，第二行是这个函数以及其子函数占用的时间
    （2）箭头代表谁调用了谁，实线箭头是直接调用，虚线箭头代表中间隔了几个不怎么耗时的无关函数。而箭头上的时间代表走这边这个子函数花了多长时间！

  火焰图（VIEW-Flame Graph）：
    （1）X轴表示 CPU 占用时间的比例。条越宽，说明这个函数消耗的 CPU 越多。
    （2）y 轴：表示调用栈的深度。从下往上，就是 A 调用 B，B 调用 C。平顶的那些小方块，就是真正把 CPU 周期吃掉的“叶子函数”。
  REFINE（提炼/过滤）：
    主要用来排除无关紧要的函数链路，比如在函数调用图中点击某个方块后，然后点击focus，就会只展示这个方框链路上的
  NOTE:网页上的cpu运行时间都是通过sample估算出来的，sample数指的是在运行期间抓拍到这个函数在运行的次数！所以不需要关注sample数，直接看估计的cpu时间就行。
  
