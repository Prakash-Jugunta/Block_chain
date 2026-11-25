# ForkRisk: Blockchain Fork Prediction Explained in Plain English 🌐

*(A beginner-friendly, layman's guide to understanding this project without needing a PhD in cryptography or distributed systems)*

---

## 1. The 30-Second Elevator Pitch

Imagine a group of 10 friends sitting in a huge, noisy auditorium trying to write a shared diary together. 

Whenever someone finishes writing a page (a **block**), they have to shout it across the room to everyone else. If the room is quiet and everyone has walkie-talkies, everyone receives the new page instantly. 

**The Problem:** What happens if the room gets noisy, people start whispering, or messages get delayed? 

- Friend A (in the front) writes **Page 10** and starts passing it backward.
- At the exact same second, Friend B (in the back) also finishes a **Page 10** because they haven't heard from Friend A yet!
- Suddenly, half the room thinks Friend A's page is next, while the other half thinks Friend B's page is next. 

This split is called a **temporary network fork**. Eventually, one page wins and the other gets thrown in the trash (**orphaned/stale**). Any transactions on the discarded page get canceled or rolled back (**reorganized**), which can allow malicious users to double-spend money!

**What this project does:**
**ForkRisk** is like a **weather forecast radar for blockchains**. By observing how slow network messages are moving and how much nodes disagree right now, our system uses Machine Learning to predict:

> *"Warning: There is an 80% chance a fork will happen within the next 3 blocks!"*

---

## 2. Why Are Temporary Forks Such a Big Deal?

In decentralized blockchains (like Bitcoin or Ethereum), there is no central boss or server. All computers (nodes) must agree on the one true history of transactions.

When a temporary fork happens, three bad things occur:

```
                  ┌──► Block 10A (Miner Alice) ──► Block 11A  (WINNER - Kept in chain)
                  │
Genesis ──► ... ──┤
                  │
                  └──► Block 10B (Miner Bob)   ──► THROWN AWAY (Stale / Orphan)
                                                   ▲
                                                   Transactions here get reverted!
```

1. **Wasted Energy & Money:** Miners/validators who worked on Block 10B did all that work for nothing.
2. **Delayed Confirmations:** Crypto exchanges and apps have to make users wait for 10–30 minutes before accepting a payment, just to make sure the block won't get canceled later.
3. **Double-Spending Attacks:** A scammer can buy a laptop with Bitcoin on Block 10B, take the laptop, and if Block 10A wins and doesn't include that payment, the scammer gets their coins back!

---

## 3. How Does ForkRisk Work? (The 4-Step Pipeline)

Our system is built completely from scratch in the **Go (Golang)** programming language. Here is the step-by-step pipeline:

```
┌─────────────────┐       ┌─────────────────┐       ┌──────────────────┐       ┌─────────────────┐
│ 1. Virtual Sim  │ ────► │ 2. Fork Tracker │ ────► │ 3. Clue Finder   │ ────► │ 4. ML Predictor │
│ (10 Nodes)      │       │ (LCA Algorithm) │       │ (Causal Features)│       │ (LR & RF Models)│
└─────────────────┘       └─────────────────┘       └──────────────────┘       └─────────────────┘
 Simulates network         Catches whenever          Gathers 22 network         Predicts forks in
 traffic & delays          nodes disagree            clues without cheating     the next 3 blocks
```

### Step 1: The Blockchain Flight Simulator (`internal/engine`)

Instead of spending thousands of dollars running real computers across the globe, we built a **deterministic flight simulator** in Go.

- It simulates 10 nodes connected in a peer-to-peer mesh.
- It models real-world chaos using configurable "stress dials":
  - **Network Latency:** Messages taking 50ms vs. 300ms.
  - **Node Churn:** Computers turning on and off or losing internet connection.
  - **Transaction Floods:** Hundreds of transactions piling up in node queues (mempools).
- **Why "Deterministic"?** If you run the simulator with Seed #42 today, tomorrow, or 5 years from now, it produces the exact same sequence of events down to the single millisecond.

### Step 2: The Fork Detective (`internal/fork`)

As blocks fly across the simulated network, our detector watches all node tips.

- When two different blocks appear at the same height, it shouts: *"Fork born!"*
- It uses a tree search algorithm called **Lowest Common Ancestor (LCA)** to trace both blocks backward until it finds where they split, calculating the exact rollback depth.

### Step 3: The Clue Collector — Feature Extraction (`internal/features`)

Every time a new block is about to be proposed, our feature extractor takes a snapshot of the network's health. It records **22 distinct clues**, such as:

- How long did the last 10 blocks take to propagate?
- How many nodes currently disagree on what the newest block is?
- What percentage of nodes are currently offline?
- How full are the transaction mempools?

> ⚠️ **Zero Cheating (No Data Leakage):** The model is strictly forbidden from looking into the future. It can only use clues that were visible at or before the exact microsecond of the proposal.

### Step 4: The Machine Learning Brain (`internal/ml`)

We feed these clues into two Machine Learning models written directly in Go (no Python libraries used):

1. **Logistic Regression:** A fast, mathematical probability calculator.
2. **Random Forest:** A committee of decision trees that vote on whether a fork is imminent.

**The Target ($H=3$):** 
At block $N$, the model answers: *"Will at least one fork birth happen between block $N+1$ and block $N+3$?"*

---

## 4. The Interactive Web Dashboard 🖥️

To make the research intuitive to explore, the project includes an interactive web dashboard running on a local Go web server (`http://localhost:8081`):

- **Overview:** Shows real-time statistics across 240 simulated experiments (~2.4 million blocks analyzed).
- **Run Inspector:** Lets you pick any experiment and inspect how nodes behaved.
- **Node Status Grid:** Shows the active block height and newest block hash for all 10 nodes. You can watch them fall out of sync during high network latency!
- **Fork Timeline:** An event-by-event history of every fork competition, orphaned block, and chain reorganization.
- **Scenario Comparison:** Visual bar and line charts comparing calm conditions against network stress.

---

## 5. What Were the Key Discoveries?

From running **240 complete production experiments**, here is what the data proved:

1. **Propagation Delay is the #1 Culprit:** 
   When block propagation delay is low (100ms) compared to block intervals (1.0s), the fork rate is almost **0%**. But when propagation delay reaches 800ms, nodes don't hear about new blocks in time, and fork rates surge past **20%**.
2. **Goodput Drops Under Stress:** 
   Even if a network is processing 10 transactions per second (TPS), when forks happen, half of those blocks get thrown away. The **Finalized Goodput** (valid, permanent transactions) drops significantly.
3. **Machine Learning Really Works:** 
   - A naive guesser (always predicting "no fork") gets an accuracy score of zero on catching forks.
   - Our pure-Go Random Forest achieves an **ROC-AUC of ~0.83**, meaning it reliably warns of upcoming forks before they occur.
4. **Sudden Network Shifts Can Be Detected in Real Time:**
   Using a statistical tool called the **Kolmogorov-Smirnov (KS) drift detector**, our system detected a sudden mid-run internet slowdown with **100% accuracy** within just a few blocks of the shift.

---

## 6. Jargon Buster: Terms Translated to Plain English

| Technical Term                   | Everyday Translation                                                                                            |
|:-------------------------------- |:--------------------------------------------------------------------------------------------------------------- |
| **Node**                         | A computer participating in the blockchain network.                                                             |
| **Block**                        | A batch of verified transactions packaged together.                                                             |
| **Block Height**                 | The page number of the blockchain (e.g., Block #100 is page 100).                                               |
| **Mempool**                      | The "waiting room" where transactions sit before getting picked into a block.                                   |
| **Gossip Protocol**              | Word-of-mouth communication where each computer whispers new blocks to its neighbors.                           |
| **Tip**                          | What a specific computer currently believes is the latest block in the chain.                                   |
| **Stale / Orphan Block**         | A valid block that lost the race and was discarded because another branch grew longer.                          |
| **Chain Reorganization (Reorg)** | When a computer realizes another chain is longer, throws away its recent blocks, and switches tracks.           |
| **LCA (Lowest Common Ancestor)** | The last block where two competing branches agreed before splitting apart.                                      |
| **Goodput**                      | The rate of transactions that actually stay permanently in the blockchain (excluding discarded blocks).         |
| **Horizon ($H=3$)**              | How far into the future we are trying to predict (looking ahead 3 blocks).                                      |
| **ROC-AUC**                      | A grade from 0.5 to 1.0 measuring how well the AI distinguishes between "safe" and "danger". (~0.8+ is strong). |

---

## 7. How to Explain This Project in an Interview or Viva Exam

If an evaluator asks you: **"What did you build and why does it matter?"**, here is your golden script:

> *"We built an end-to-end framework in pure Go to study and predict network-induced blockchain forks.*
> 
> *In decentralized networks, when internet latency spikes or nodes disconnect, multiple computers propose competing blocks at the same height because messages don't travel fast enough. This leads to chain reorganizations, wasted compute, and security vulnerabilities like double-spending.*
> 
> *Our project has four key contributions:*
> 
> 1. *A **deterministic discrete-event simulator** in Go that models 10 nodes gossiping blocks under network stress.*
> 2. *An **LCA fork detector** that accurately tracks fork births, orphaned blocks, and rollback depths.*
> 3. *A **causal feature engineering engine** that extracts 22 network health signals with zero future data leakage.*
> 4. *A **pure-Go Machine Learning pipeline** (Logistic Regression & Random Forest) that predicts whether a fork will happen within the next 3 blocks with an ROC-AUC above 0.82.*
> 
> *Everything is visualized in an interactive web dashboard and validated across 240 production runs without using any Python or external dependencies."*

---

## 8. Frequently Asked Questions (FAQ)

### Q: Why is Python not used anywhere in this project?

**A:** Python simulators rely on external libraries and often suffer from the Global Interpreter Lock (GIL) and non-deterministic concurrency. By writing everything in Go, we achieved a single self-contained application, high performance, and bit-for-bit determinism where the exact same random seed always produces the exact same block sequence.

### Q: Does the machine learning model stop the forks from happening?

**A:** Not in this case study phase — here, the model acts as an **observational early-warning radar**. However, this case study forms the foundation for our end-semester project, where we will build an **adaptive consensus controller** that automatically slows down block proposals or adjusts transaction limits whenever the model warns of high fork risk!

### Q: What is the difference between Offered TPS and Goodput?

**A:** Offered TPS is how many transactions are submitted to the network. Goodput is how many transactions actually survive permanently on the winning blockchain. When network latency is high, Offered TPS might stay high, but Goodput collapses because blocks are constantly getting discarded in forks.

---

*Created for the ForkRisk Case Study — Milestone 1 to 6 Implementation.*
