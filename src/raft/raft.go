package main

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

// how to send and receive RPCs.

import (
	"6.824/labgob"
	"bytes"
	//	"bytes"
	"sync"
	"sync/atomic"
	"time"

	//	"6.824/labgob"
	"6.824/labrpc"
)

// ApplyMsg
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in part 2D you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh, but set CommandValid to false for these
// other uses.
//
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int

	// For 2D:
	SnapshotValid bool
	Snapshot      []byte
	SnapshotTerm  int
	SnapshotIndex int
}

type LogEntry struct {
	Command interface{}
	Term    int
}

// Raft
// A Go object implementing a single Raft peer.
//
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.
	currentTerm    int
	votedFor       int
	heartbeatTimer *time.Timer
	electionTimer  *time.Timer
	state          StatePeer

	logEntries  []*LogEntry
	commitIdx   int
	lastApplied int
	nextIdx     []int
	matchIdx    []int
	applyCh     chan ApplyMsg
}

func (rf *Raft) ConvertState(state StatePeer) {
	if rf.state == state {
		return
	}
	//DPrintf("[term=%d;server=%d] convert from %+v to %+v\n", rf.currentTerm, rf.me, rf.state, state)

	rf.state = state

	if state == Follower {
		rf.heartbeatTimer.Stop()
		ResetTimer(rf.electionTimer, ElectionTimeDuration())
		rf.votedFor = -1
	} else if state == Candidate {
		rf.StartElection()
	} else if state == Leader {
		rf.electionTimer.Stop()

		rf.nextIdx=make([]int,len(rf.peers))
		rf.matchIdx=make([]int,len(rf.peers))
		if len(rf.logEntries) == 0{
			rf.logEntries=append(rf.logEntries,&LogEntry{
				Command: nil,
				Term:    0,
			})
		}
		for i := range rf.nextIdx {
			rf.nextIdx[i] = len(rf.logEntries)
		}

		rf.BroadcastHeartbeat()
		ResetTimer(rf.heartbeatTimer, HeartbeatInterval)
	}

}

func (rf *Raft) BroadcastHeartbeat() {
	if rf.state != Leader {
		return
	}

	for peer := range rf.peers {
		if peer == rf.me {
			continue
		}
		go func(server int) {
			rf.mu.Lock()
			if rf.state != Leader {
				rf.mu.Unlock()
				return
			}

			DPrintf("[term=%+v;server=%+v;nextIdx=%+v;matchIdx=%+v;lastCommitIdx=%+v;lastApplied=%+v] broadcast heartbeat\n", rf.currentTerm, rf.me, rf.nextIdx, rf.matchIdx, rf.commitIdx, rf.lastApplied)

			prevLogIdx := rf.nextIdx[server] - 1
			prevLogTerm := rf.logEntries[prevLogIdx].Term
			storeLogEntries := make([]*LogEntry, len(rf.logEntries[prevLogIdx+1:]))
			copy(storeLogEntries, rf.logEntries[prevLogIdx+1:])

			args := AppendEntriesArgs{
				Term:            rf.currentTerm,
				LeaderId:        rf.me,
				PrevLogIdx:      prevLogIdx,
				PrevLogTerm:     prevLogTerm,
				LogEntries:      storeLogEntries,
				LeaderCommitIdx: rf.commitIdx,
			}

			rf.mu.Unlock()
			reply := AppendEntriesReply{}
			if rf.sendAppendEntries(server, &args, &reply) {
				rf.mu.Lock()
				defer rf.mu.Unlock()

				if rf.state != Leader {
					return
				}

				if reply.Success {
					matchIdx := args.PrevLogIdx + len(args.LogEntries)
					rf.matchIdx[server] = matchIdx
					rf.nextIdx[server] = matchIdx + 1

					for i := len(rf.logEntries) - 1; i > rf.commitIdx; i-- {
						count := 0
						for _, matchIdx = range rf.matchIdx {
							if matchIdx >= i {
								count++
							}
						}

						if count > len(rf.peers)/2 {
							rf.SetCommitIdx(i)
							break
						}
					}

				} else {
					if reply.Term > rf.currentTerm {
						rf.currentTerm = reply.Term
						rf.ConvertState(Follower)
						rf.persist()
					} else {
						if reply.ConflictLogTerm != rf.logEntries[reply.ConflictLogIdx].Term {
							for rf.logEntries[reply.ConflictLogIdx-1].Term != reply.ConflictLogTerm {
								reply.ConflictLogIdx--
								if reply.ConflictLogIdx <= 1 {
									break
								}
							}
						}
						rf.nextIdx[server] = reply.ConflictLogIdx
					}
				}

			}
		}(peer)

	}

}

func (rf *Raft) StartElection() {
	defer rf.persist()
	rf.currentTerm += 1
	rf.votedFor = rf.me
	lastLogIdx := len(rf.logEntries) - 1
	lastLogTerm := rf.logEntries[lastLogIdx].Term
	args := RequestVoteArgs{
		Term:        rf.currentTerm,
		CandidateId: rf.me,
		LastLogIdx:  lastLogIdx,
		LastLogTerm: lastLogTerm,
	}

	var voteCount int32

	for peer := range rf.peers {
		if peer == rf.me {
			atomic.AddInt32(&voteCount, 1)
			continue
		}

		go func(server int) {
			reply := RequestVoteReply{}
			if rf.sendRequestVote(server, &args, &reply) {
				rf.mu.Lock()
				defer rf.mu.Unlock()

				//DPrintf("[server=%+v]got RequestVote response:VoteGranted=%v, Term=%d", server, reply.VoteGranted, reply.Term)

				if reply.VoteGranted && rf.state == Candidate {
					atomic.AddInt32(&voteCount, 1)
					if atomic.LoadInt32(&voteCount) > int32(len(rf.peers)/2) {
						rf.ConvertState(Leader)
					}
					return
				}

				if reply.Term > rf.currentTerm {
					rf.currentTerm = reply.Term
					rf.ConvertState(Follower)
					rf.persist()
				}
			}
		}(peer)
	}

}

func (rf *Raft) SetCommitIdx(idx int) {
	//DPrintf("[term=%+v;server=%+v;nextIdx=%+v;matchIdx=%+v;lastCommitIdx=%+v;lastApplied=%+v;commitIdx=%+v] set commit index\n", rf.currentTerm, rf.me, rf.nextIdx, rf.matchIdx, rf.commitIdx, rf.lastApplied, idx)
	rf.commitIdx = idx
	if rf.commitIdx > rf.lastApplied {
		applyEntries := make([]*LogEntry, 0, rf.commitIdx-rf.lastApplied)
		applyEntries = append(applyEntries, rf.logEntries[rf.lastApplied+1:rf.commitIdx+1]...)

		go func(applyIdx int, entries []*LogEntry) {
			for k, v := range entries {
				msg := ApplyMsg{
					CommandValid: true,
					Command:      v.Command,
					CommandIndex: applyIdx + k,
				}
				rf.applyCh <- msg
				rf.mu.Lock()
				if rf.lastApplied < msg.CommandIndex {
					rf.lastApplied = msg.CommandIndex
				}
				rf.mu.Unlock()
			}
		}(rf.lastApplied+1, applyEntries)
	}
}

// GetState return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	// Your code here (2A).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.currentTerm, rf.state == Leader
}

//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// data := w.Bytes()
	// rf.persister.SaveRaftState(data)

	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	if e.Encode(rf.currentTerm) != nil || e.Encode(rf.votedFor) != nil || e.Encode(rf.logEntries) != nil {
		return
	}
	data := w.Bytes()
	rf.persister.SaveRaftState(data)

}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (2C).
	// Example:
	// r := bytes.NewBuffer(data)
	// d := labgob.NewDecoder(r)
	// var xxx
	// var yyy
	// if d.Decode(&xxx) != nil ||
	//    d.Decode(&yyy) != nil {
	//   error...
	// } else {
	//   rf.xxx = xxx
	//   rf.yyy = yyy
	// }

	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var currentTerm int
	var logEntries []*LogEntry
	var votedFor int
	if d.Decode(&currentTerm) != nil || d.Decode(&votedFor) != nil || d.Decode(&logEntries) != nil {
		return
	}

	rf.currentTerm = currentTerm
	rf.votedFor = votedFor
	rf.logEntries = logEntries

}

// CondInstallSnapshot
// A service wants to switch to snapshot.  Only do so if Raft hasn't
// have more recent info since it communicate the snapshot on applyCh.
//
func (rf *Raft) CondInstallSnapshot(lastIncludedTerm int, lastIncludedIdx int, snapshot []byte) bool {

	// Your code here (2D).

	return true
}

// Snapshot the service says it has created a snapshot that has
// all info up to and including index. this means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (2D).

}

// RequestVoteArgs
// example RequestVote RPC arguments structure.
// field names must start with capital letters!
//
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term        int
	CandidateId int

	LastLogIdx  int
	LastLogTerm int
}

// RequestVoteReply
// example RequestVote RPC reply structure.
// field names must start with capital letters!
//
type RequestVoteReply struct {
	// Your data here (2A).
	Term        int
	VoteGranted bool
}

// RequestVote
// example RequestVote RPC handler.
//
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	defer rf.persist()
	reply.Term = rf.currentTerm
	reply.VoteGranted = false
	if args.Term < rf.currentTerm {
		return
	}

	if args.Term == rf.currentTerm && rf.votedFor != -1 && rf.votedFor != args.CandidateId {
		return
	}

	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		reply.Term = rf.currentTerm
		rf.ConvertState(Follower)
	}

	// up-to-date
	lastLogIdx := len(rf.logEntries) - 1
	lastLogTerm := rf.logEntries[lastLogIdx].Term
	if args.LastLogTerm < lastLogTerm || (args.LastLogTerm == lastLogTerm && args.LastLogIdx < lastLogIdx) {
		return
	}

	reply.VoteGranted = true
	rf.votedFor = args.CandidateId
	rf.electionTimer.Reset(ElectionTimeDuration())
	return
}

type AppendEntriesArgs struct {
	Term     int
	LeaderId int

	PrevLogIdx      int
	PrevLogTerm     int
	LogEntries      []*LogEntry
	LeaderCommitIdx int
}

type AppendEntriesReply struct {
	Term    int
	Success bool

	ConflictLogIdx  int
	ConflictLogTerm int
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	defer rf.persist()
	reply.Term = rf.currentTerm
	reply.Success = false
	reply.ConflictLogIdx = 1
	reply.ConflictLogTerm = 1

	if rf.currentTerm < args.Term {
		rf.currentTerm = args.Term
		reply.Term = rf.currentTerm
		rf.ConvertState(Follower)
	} else if rf.currentTerm > args.Term {
		return
	}

	ResetTimer(rf.electionTimer, ElectionTimeDuration())

	lastLogIdx := len(rf.logEntries) - 1
	if lastLogIdx < args.PrevLogIdx {
		reply.ConflictLogIdx = lastLogIdx
		reply.ConflictLogTerm = rf.logEntries[lastLogIdx].Term
		return
	}

	rePrevLogTerm := rf.logEntries[args.PrevLogIdx].Term
	if rePrevLogTerm != args.PrevLogTerm {
		reply.ConflictLogTerm = rePrevLogTerm
		reply.ConflictLogIdx = args.PrevLogIdx

		for rf.logEntries[reply.ConflictLogIdx-1].Term == reply.ConflictLogTerm {
			reply.ConflictLogIdx--
			if reply.ConflictLogIdx <= 1 {
				break
			}
		}
		return
	}

	unMatchIdx := -1
	for idx := range args.LogEntries {
		if len(rf.logEntries) < (args.PrevLogIdx+2+idx) ||
			rf.logEntries[args.PrevLogIdx+1+idx].Term != args.PrevLogTerm ||
			rf.logEntries[args.PrevLogIdx+1+idx].Command != args.LogEntries[idx].Command {
			unMatchIdx = idx
			break
		}
	}

	if unMatchIdx != -1 {
		//DPrintf("[term=%+v;server=%+v;unMatchIdx=%+v;prevLogIdx=%+v] append logEntries \n", rf.currentTerm, rf.me, unMatchIdx, args.PrevLogIdx)
		rf.logEntries = rf.logEntries[:args.PrevLogIdx+1+unMatchIdx]
		rf.logEntries = append(rf.logEntries, args.LogEntries[unMatchIdx:]...)
	}

	if args.LeaderCommitIdx > rf.commitIdx {
		minIdx := min(args.LeaderCommitIdx, len(rf.logEntries)-1)
		rf.SetCommitIdx(minIdx)
	}

	reply.Success = true

	return
}

//
// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
//
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

// Start
// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
//
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := true

	// Your code here (2B).
	term, isLeader = rf.GetState()

	if isLeader {
		rf.mu.Lock()
		index = len(rf.logEntries)
		rf.logEntries = append(rf.logEntries, &LogEntry{Command: command, Term: rf.currentTerm})
		rf.persist()
		rf.matchIdx[rf.me] = index
		rf.nextIdx[rf.me] = index + 1
		//DPrintf("[term=%+v;server=%+v;nextIdx=%+v;matchIdx=%+v;lastCommitIdx=%+v;lastApplied=%+v] start\n", rf.currentTerm, rf.me, rf.nextIdx, rf.matchIdx, rf.commitIdx, rf.lastApplied)
		rf.BroadcastHeartbeat()
		rf.mu.Unlock()
	}

	return index, term, isLeader
}

// Kill
// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
//
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

// The ticker go routine starts a new election if this peer hasn't received
// heartsbeats recently.
func (rf *Raft) ticker() {
	for rf.killed() == false {

		// Your code here to check if a leader election should
		// be started and to randomize sleeping time using
		// time.Sleep().

	}
}

// Make
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
//
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here (2A, 2B, 2C).
	rf.currentTerm = 1
	rf.votedFor = -1
	rf.state = Follower
	rf.electionTimer = time.NewTimer(ElectionTimeDuration())
	rf.heartbeatTimer = time.NewTimer(HeartbeatInterval)

	rf.logEntries = make([]*LogEntry, 0, 1)
	rf.logEntries = append(rf.logEntries, &LogEntry{
		Command: nil,
		Term:    0,
	})
	rf.applyCh = applyCh

	rf.mu.Lock()
	rf.readPersist(persister.ReadRaftState())
	rf.mu.Unlock()

	rf.nextIdx = make([]int, len(rf.peers))
	rf.matchIdx = make([]int, len(rf.peers))
	for i := range rf.peers {
		rf.nextIdx[i] = len(rf.logEntries)
	}

	go func(peer *Raft) {
		for {
			select {
			case <-peer.heartbeatTimer.C:
				peer.mu.Lock()
				if peer.state == Leader {
					peer.BroadcastHeartbeat()
					peer.heartbeatTimer.Reset(HeartbeatInterval)
				}
				peer.mu.Unlock()
			case <-peer.electionTimer.C:
				peer.mu.Lock()
				peer.electionTimer.Reset(ElectionTimeDuration())
				if peer.state == Follower {
					peer.ConvertState(Candidate)
				} else if peer.state == Candidate {
					peer.StartElection()
				}
				peer.mu.Unlock()
			}
		}
	}(rf)

	//// start ticker goroutine to start elections
	//go rf.ticker()

	return rf
}
