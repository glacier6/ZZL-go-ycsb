实验室超级服务器
CPU： 
执行指令：lscpu
  Architecture:                    x86_64
  CPU op-mode(s):                  32-bit, 64-bit
  Byte Order:                      Little Endian
  Address sizes:                   46 bits physical, 57 bits virtual
  CPU(s):                          144
  On-line CPU(s) list:             0-143
  Thread(s) per core:              2
  Core(s) per socket:              36
  Socket(s):                       2
  NUMA node(s):                    2
  Vendor ID:                       GenuineIntel
  CPU family:                      6
  Model:                           106
  Model name:                      Intel(R) Xeon(R) Platinum 8352V CPU @ 2.10GHz
  Stepping:                        6
  CPU MHz:                         2100.000
  CPU max MHz:                     3500.0000
  CPU min MHz:                     800.0000
  BogoMIPS:                        4200.00
  Virtualization:                  VT-x
  L1d cache:                       3.4 MiB
  L1i cache:                       2.3 MiB
  L2 cache:                        90 MiB
  L3 cache:                        108 MiB
  NUMA node0 CPU(s):               0-35,72-107
  NUMA node1 CPU(s):               36-71,108-143


内存： 
执行指令：sudo dmidecode -t memory
总内存	16 × 64GB = 1024 GB (1TB)
NUMA 节点	8 个 (NODE 0~7)，每节点 2 根 DIMM
内存类型	DDR4-3200 RDIMM (实际跑 2933 MT/s)
颗粒	Samsung M393A8G40AB2-CWE，2Rank
ECC	72bit = 64bit 数据 + 8bit ECC
可扩容	最大支持 6TB

硬盘：
执行指令： sudo lshw -class disk
          cat /sys/block/nvme0n1/device/model
INTEL SSDPE2KX040T8
英特尔 DC P4510 4TB 企业级 NVMe 固态硬盘



我的小主机
CPU： 
  架构：                    x86_64
  CPU 运行模式：          32-bit, 64-bit
  Address sizes:          39 bits physical, 48 bits virtual
  字节序：                Little Endian
CPU:                      4
  在线 CPU 列表：         0-3
厂商 ID：                 GenuineIntel
  BIOS Vendor ID:         Intel(R) Corporation
  型号名称：              Intel(R) Core(TM) i5-7500 CPU @ 3.40GHz
    BIOS Model name:      Intel(R) Core(TM) i5-7500 CPU @ 3.40GHz To Be Filled By O.E.M. CPU @ 3.4GHz
    BIOS CPU family:      205
    CPU 系列：            6
    型号：                158
    每个核的线程数：      1
    每个座的核数：        4
    座：                  1
    步进：                9
    CPU(s) scaling MHz:   95%
    CPU 最大 MHz：        3800.0000
    CPU 最小 MHz：        800.0000
    BogoMIPS：            6799.81

内存：
  Memory Device
    Array Handle: 0x0009
    Error Information Handle: Not Provided
    Total Width: 64 bits
    Data Width: 64 bits
    Size: 16 GB
    Form Factor: SODIMM
    Set: None
    Locator: DIMM1
    Bank Locator: Not Specified
    Type: DDR4
    Type Detail: Synchronous Unbuffered (Unregistered)
    Speed: 3200 MT/s
    Manufacturer: 802C0000802C
    Serial Number: 00971E98
    Asset Tag: 01234000
    Part Number: MTA8ATF2G64HZ-3G2E1 
    Rank: 1
    Configured Memory Speed: 2400 MT/s
    Minimum Voltage: Unknown
    Maximum Voltage: Unknown
    Configured Voltage: 1.2 V

  Handle 0x000B, DMI type 17, 40 bytes
  Memory Device
    Array Handle: 0x0009
    Error Information Handle: Not Provided
    Total Width: 64 bits
    Data Width: 64 bits
    Size: 16 GB
    Form Factor: SODIMM
    Set: None
    Locator: DIMM2
    Bank Locator: Not Specified
    Type: DDR4
    Type Detail: Synchronous Unbuffered (Unregistered)
    Speed: 3200 MT/s
    Manufacturer: 802C0000802C
    Serial Number: 00971E98
    Asset Tag: 01234000
    Part Number: MTA8ATF2G64HZ-3G2E1 
    Rank: 1
    Configured Memory Speed: 2400 MT/s
    Minimum Voltage: Unknown
    Maximum Voltage: Unknown
    Configured Voltage: 1.2 V


硬盘：
  HYV1TBX3(HXY)






实验室超级服务器运行时top输出
top - 16:19:28 up 47 days, 4 min,  5 users,  load average: 15.23, 20.32, 20.83
Tasks: 2332 total,   1 running, 2331 sleeping,   0 stopped,   0 zombie
%Cpu(s):  7.4 us,  1.5 sy,  0.0 ni, 91.1 id,  0.0 wa,  0.0 hi,  0.0 si,  0.0 st
MiB Mem : 1031691.+total, 312715.0 free, 419946.3 used, 299030.3 buff/cache
MiB Swap:      0.0 total,      0.0 free,      0.0 used. 602349.4 avail Mem 

    PID USER      PR  NI    VIRT    RES    SHR S  %CPU  %MEM     TIME+ COMMAND                                  
3042379 libvirt+  20   0  196.7g 192.2g  23284 S 471.2  19.1  46230:01 qemu-system-x86                          
2771656 libvirt+  20   0  262.6g 256.2g  23432 S 398.4  25.4 219752:29 qemu-system-x86                          
1600620 zlzhao    20   0   22.0g  15.2g  11.4g S 176.1   1.5  21:55.58 go-ycsb                                  
  10306 root      20   0 9398276 177120  63648 S  85.6   0.0  10127:37 kubelet                                  
   4835 zlh       20   0  812328  24312  11280 S  47.4   0.0  21775:40 node_exporter                            
  19030 systemd+  20   0 2632884 200708   3760 S  19.6   0.0   8357:42 beam.smp                                 
  16744 root      20   0  156780  55264  30052 S   7.8   0.0   2984:23 calico-node                              
   7104 root      20   0 1264284 492548  66616 S   7.2   0.0   7134:09 kube-apiserver                           
   3840 root      20   0   12.1g 142696  58188 S   3.9   0.0   7980:57 dockerd                                  
  25175 root      20   0   38.7g  12.8g  10544 S   3.6   1.3   4147:21 java                                     
  25176 root      20   0   35864  31308   1412 S   3.6   0.0   1211:08 nfs-auto-mount.                          
   7455 root      20   0   10.2g 115120  21372 S   2.3   0.0   1735:01 etcd                                     
   7105 root      20   0  309708 128164  59016 S   1.6   0.0   1581:14 kube-controller                          
1630163 zlzhao    20   0   23324   6904   3812 R   1.6   0.0   0:00.27 top                                      
    888 root      25   5       0      0      0 S   1.3   0.0  11375:53 ksmd                                     
   5126 root      20   0   13.2g 369008  17880 S   1.3   0.0 136429:43 cadvisor                                 
   2825 root      20   0 7125040  67580  37668 S   0.7   0.0 879:18.21 containerd                               
   5127 10000     20   0 4992220  57228  40272 S   0.7   0.0 244:28.83 harbor_jobservi                          
   6315 root      20   0 2189272   6368   2644 S   0.7   0.0 333:28.95 docker-proxy                             
   6396 nobody    20   0 7616768 784700  80656 S   0.7   0.1   2953:51 prometheus  