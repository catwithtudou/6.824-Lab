# Raft Lab

## 2A

> Implement Raft leader election and heartbeats (`AppendEntries` RPCs with no log entries). The goal for Part 2A is for a single leader to be elected, for the leader to remain the leader if there are no failures, and for a new leader to take over if the old leader fails or if packets to/from the old leader are lost. Run `go test -run 2A -race` to test your 2A code.

2A 虽然只涉及到 Raft 程序的部分功能，但是决定了整个 Lab 的程序结构，所以我们在设计时尽量考虑到结构的通用性。

参考论文的 Figure2 中可以很清晰地看到 Raft 的主要功能设计：

![image-20210625004640059](http://img.zhengyua.cn/20210625004640.png)

再考虑到 Lab 代码实现的部分，所以我们设计入手的重点可以从下面内容开始：

- Raft Structure
- State(Follow/Candidate/Leader) Change
- AppendEntries RPC / RequestVote RPC
- Period Time Ticker(Election/Heartbeats)

首先我们可以考虑每个状态下的 Raft 节点可能会处理的事件：

![image-20210624233354354](http://img.zhengyua.cn/20210624233400.png)

而每个状态的转变可以参考论文中的 Figure4：

![image-20210624233516098](http://img.zhengyua.cn/20210624233516.png)

通过上面的分析我们可以发现该 Lab 的代码结构需要合理地组织上面各个状态需要处理的事件和转变的时机。

我们在代码实现的时候需要注意几点：

- 由于 Lab 很多地方都是利用协程做到并发，所以还需要考虑 goroutine 之间的冲突；

- Lab 中所要求的`labrpc`在调取 RPC 的时候是通过开启一个 goroutine 进行处理的，所以我们需要注意在调用时若涉及到线程不安全的地方则需要进行加锁保护；
-  Lab 中需要的周期检查即 Period Time Ticker，需要我们在`Make()`的时候开启 goroutine 进行`select`处理，且处理时也会并行调用 RPC。

> Hint:
>
> - You can't easily run your Raft implementation directly; instead you should run it by way of the tester, i.e. `go test -run 2A -race`.
> - Follow the paper's Figure 2. At this point you care about sending and receiving RequestVote RPCs, the Rules for Servers that relate to elections, and the State related to leader election,
> - Add the Figure 2 state for leader election to the `Raft` struct in `raft.go`. You'll also need to define a struct to hold information about each log entry.
> - Fill in the `RequestVoteArgs` and `RequestVoteReply` structs. Modify `Make()` to create a background goroutine that will kick off leader election periodically by sending out `RequestVote` RPCs when it hasn't heard from another peer for a while. This way a peer will learn who is the leader, if there is already a leader, or become the leader itself. Implement the `RequestVote()` RPC handler so that servers will vote for one another.
> - To implement heartbeats, define an `AppendEntries` RPC struct (though you may not need all the arguments yet), and have the leader send them out periodically. Write an `AppendEntries` RPC handler method that resets the election timeout so that other servers don't step forward as leaders when one has already been elected.
> - Make sure the election timeouts in different peers don't always fire at the same time, or else all peers will vote only for themselves and no one will become the leader.
> - The tester requires that the leader send heartbeat RPCs no more than ten times per second.
> - The tester requires your Raft to elect a new leader within five seconds of the failure of the old leader (if a majority of peers can still communicate). Remember, however, that leader election may require multiple rounds in case of a split vote (which can happen if packets are lost or if candidates unluckily choose the same random backoff times). You must pick election timeouts (and thus heartbeat intervals) that are short enough that it's very likely that an election will complete in less than five seconds even if it requires multiple rounds.
> - The paper's Section 5.2 mentions election timeouts in the range of 150 to 300 milliseconds. Such a range only makes sense if the leader sends heartbeats considerably more often than once per 150 milliseconds. Because the tester limits you to 10 heartbeats per second, you will have to use an election timeout larger than the paper's 150 to 300 milliseconds, but not too large, because then you may fail to elect a leader within five seconds.
> - You may find Go's [rand](https://golang.org/pkg/math/rand/) useful.
> - You'll need to write code that takes actions periodically or after delays in time. The easiest way to do this is to create a goroutine with a loop that calls [time.Sleep()](https://golang.org/pkg/time/#Sleep); (see the `ticker()` goroutine that `Make()` creates for this purpose). Don't use Go's `time.Timer` or `time.Ticker`, which are difficult to use correctly.
> - The [Guidance page](https://pdos.csail.mit.edu/6.824/labs/guidance.html) has some tips on how to develop and debug your code.
> - If your code has trouble passing the tests, read the paper's Figure 2 again; the full logic for leader election is spread over multiple parts of the figure.
> - Don't forget to implement `GetState()`.
> - The tester calls your Raft's `rf.Kill()` when it is permanently shutting down an instance. You can check whether `Kill()` has been called using `rf.killed()`. You may want to do this in all loops, to avoid having dead Raft instances print confusing messages.
> - Go RPC sends only struct fields whose names start with capital letters. Sub-structures must also have capitalized field names (e.g. fields of log records in an array). The `labgob` package will warn you about this; don't ignore the warnings.

## 2B

> Implement the leader and follower code to append new log entries, so that the `go test -run 2B -race` tests pass.

该部分则需要我们在 2A 的基础上实现 leader 与 follow 之间关于 Log Entry 的相关功能，最终实现节点之间日志的一致性。

这部分实现的重点就在于 AppendEntriesRPC 和 Start 函数。


