package kernel

import (
	"unsafe"

	"github.com/blacksystemcoder/letsgo-os/kernel/log"
	"github.com/blacksystemcoder/letsgo-os/kernel/mm"
)

type stack struct {
	hi uintptr
	lo uintptr
}

type taskswitchbuf struct {
	sp uintptr
}

var (
	CurrentThread  *Thread    = &scheduleThread
	CurrentDomain  *Domain    = nil
	allDomains     domainList = domainList{head: nil, tail: nil}
	largestPid     uint32     = 0x0
	kernelHlt      bool       = false
	scheduleThread Thread     = Thread{}
)

func backupFpRegs(buffer uintptr)
func restoreFpRegs(buffer uintptr)

func AddDomain(d *Domain) {
	allDomains.Append(d)
	if ENABLE_DEBUG {
		log.KDebugLn("Added new domain with pid ", d.pid)
	}
	if CurrentDomain == nil || CurrentThread == nil {
		CurrentDomain = allDomains.head
		CurrentThread = CurrentDomain.runningThreads.thread
	}
}

func ExitDomain(d *Domain) {
	allDomains.Remove(d)

	if allDomains.head == nil {
		CurrentDomain = nil
	}

	// clean up memory
	scheduleStackArg(func(dom uintptr) {
		doma := (*Domain)(unsafe.Pointer(dom))
		cleanUpDomain(doma)
	}, (uintptr)(unsafe.Pointer(d)))
}

// Execute on scheduleStack
func cleanUpDomain(d *Domain) {
	// Clean up threads
	for cur := d.runningThreads.thread; d.runningThreads.thread != nil; cur = d.runningThreads.thread {
		//log.KDebugln("Clean up thread ", cur.tid)
		//log.KDebugln("t:", (uintptr)(unsafe.Pointer(cur)), " t.n:", (uintptr)(unsafe.Pointer(cur.next)), " t.p:", (uintptr)(unsafe.Pointer(cur.prev)))
		d.runningThreads.Dequeue(cur)
		cleanUpThread(cur)
	}
	for cur := d.blockedThreads.thread; d.blockedThreads.thread != nil; cur = d.blockedThreads.thread {
		cleanUpThread(cur)
		d.blockedThreads.Dequeue(cur)
	}
	// Clean allocated memory
	d.MemorySpace.FreeAllPages()

	// Clean up kernel resources
	if ENABLE_DEBUG {
		log.KDebugLn("Allocated pages ", mm.AllocatedPages, " (out of", maxPages, ")")
	}
	Schedule()
	mm.FreePage((uintptr)(unsafe.Pointer(d)))
}

// Execute on scheduleStack
func cleanUpThread(t *Thread) {
	// TODO; Adjust when thread control block is no longer a single page
	threadPtr := (uintptr)(unsafe.Pointer(t))
	threadDomain := t.domain
	threadDomain.MemorySpace.UnmapPage(t.kernelStack.lo)
	threadDomain.MemorySpace.UnmapPage(threadPtr)
	if CurrentThread == t {
		CurrentThread = nil
	}
}

func ExitThread(t *Thread) {
	if t.domain.numThreads <= 1 {
		// we're last thread
		ExitDomain(t.domain) // does not return
	}
	t.domain.RemoveThread(t)
	if ENABLE_DEBUG {
		log.KDebugLn("Removing thread ", t.tid, " from domain ", t.domain.pid)
	}
	scheduleStackArg(func(threadPtr uintptr) {
		thread := (*Thread)(unsafe.Pointer(threadPtr))
		cleanUpThread(thread)
		Schedule()
	}, (uintptr)(unsafe.Pointer(t)))
}

func BlockThread(t *Thread) {
	t.isBlocked = true
	t.domain.runningThreads.Dequeue(t)
	t.domain.blockedThreads.Enqueue(t)
	PerformSchedule = true
}

func getESP() uintptr
func waitForInterrupt()

func Block() {
	waitForInterrupt()
}

func ResumeThread(t *Thread) {
	t.isBlocked = false
	t.domain.blockedThreads.Dequeue(t)
	t.domain.runningThreads.Enqueue(t)
}

func Schedule() {
	if CurrentDomain == nil {
		log.KErrorLn("No Domains to schedule")
		Shutdown()
		// DisableInterrupts()
		// Hlt()
	}
	//log.KDebug("Scheduling in ")
	//printTid(defaultLogWriter, currentThread)
	nextDomain := CurrentDomain.next
	newThread := nextDomain.runningThreads.Next()
	if newThread == nil {
		for newDomain := nextDomain.next; newDomain != nextDomain; newDomain = newDomain.next {
			newThread = newDomain.runningThreads.Next()
			if newThread != nil {
				break
			}
		}
	}
	//if currentThread.next == currentThread && currentThread.tid != 0{
	//    kernelPanic("Why no next?")
	//}
	//if newThread.tid == currentThread.tid && currentThread.tid != 0 {
	//    kernelPanic("Why only one thread?")
	//}
	//log.KPrintln("next domain: ", nextDomain.pid)
	//log.KPrintln("next thread: ", newThread.tid)
	//log.KPrintln("domain threads: ", nextDomain.numThreads)

	if newThread == nil {
		if kernelHlt {
			// We are already stalling the kernel
			return
		}
		// All threads blocked or no threads exist anymore.
		kernelHlt = true
		PerformSchedule = false
		//currentThread = nil
		kernelPanic("test")
		return
	}

	kernelHlt = false
	CurrentDomain = nextDomain
	if newThread == CurrentThread {
		return
	}

	switchToThread(newThread)

	//log.KDebug("Now executing: ")
	//printTid(defaultLogWriter, currentThread)
}

func switchToThread(t *Thread) {
	// Save state of current thread
	if CurrentThread != nil {
		addr := uintptr(unsafe.Pointer(&(CurrentThread.fpState)))
		offset := 16 - (addr % 16)
		CurrentThread.fpOffset = offset

		backupFpRegs(addr + offset)
	}

	// Load next thread
	//log.KDebugln("Switching to domain pid", currentDomain.pid, " and thread ", t.tid)
	CurrentThread = t

	addr := uintptr(unsafe.Pointer(&(CurrentThread.fpState)))
	offset := CurrentThread.fpOffset
	if offset != 0xffffffff {
		if (addr+offset)%16 != 0 {
			log.KPrintLn(addr, " ", offset)
			kernelPanic("Cannot restore FP state. Not aligned. Did array move?")
		}
		restoreFpRegs(addr + offset)
	}

	// Load TLS
	FlushTlsTable(t.tlsSegments[:])
}

func InitScheduling() {

}
