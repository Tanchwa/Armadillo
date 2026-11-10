# Armadillo — design notes

Status: **plan, not yet built.** Nothing here is committed to; it is the state
of the design as worked out before scaffolding. Captured 2026-11-06.

Armadillo is a Kubernetes operator that pairs with
[`apparmor-profile-action`](https://github.com/Tanchwa/apparmor-profile-action).
The action learns an AppArmor profile in CI and commits it to the application
repo. Armadillo does the two things CI cannot: it observes what the workload
actually does in a real cluster, and it gets the resulting profile onto nodes.

It is meant to do double duty depending on where it runs:

| Cluster | Role |
|---|---|
| test / staging | Harvest complain-mode audit records, feed them back as PRs |
| production | Distribute committed profiles to nodes, enforce, demote on failure |

---

## 1. Why this exists

A profile learned in CI is a **lower bound**, not a policy. One learning run
only observes the code paths it exercised, and a container started by
`docker run` on a GitHub runner never touches most of what the same process
touches in a cluster:

- projected serviceaccount tokens
- the resolver config the kubelet wrote
- the CA bundle, under whatever rootfs the node actually has
- Python modules imported lazily the first time an error path runs
- anything that only exists because something else mounted it alongside

So the profile has to **accumulate across environments**. `merge: true` in the
action (v1.1+) is the mechanism; Armadillo is what feeds it from the only
environment that can produce the missing records.

## 2. The one structural rule

> **Decide cluster-wide. Apply per node.**

Nodes see different traffic. If promotion were decided per node, a node that
happened to run the workload only during a drain would learn a startup-only
profile, go quiet, and promote itself to enforce. The scheduler then moves a
real pod there and it dies.

The union of audit records across all nodes is the input to one decision.
Nodes are where policy gets **loaded**, never where it gets **decided**.

## 3. Components

- **Collector (DaemonSet).** Reads AppArmor audit records on each node and
  ships them to the controller. Must use `auditd`/`audispd` rather than
  scraping `dmesg` — see §6.
- **Controller.** Holds the per-workload union of records, owns the lifecycle
  state machine, files PRs back to the application repo, decides promotion.
- **Loader (DaemonSet, possibly the same pod as the collector).** Writes
  profiles to the node, runs `apparmor_parser -r`, labels the node with what
  it loaded.
- **Admission webhook.** Injects the readiness gate (§5) into pods for
  workloads under management.

## 4. Profile lifecycle

```
  learning  ──▶  candidate  ──▶  enforcing
      ▲                              │
      └──────────────────────────────┘
               any DENIED
```

### learning
Complain mode everywhere. Logs violations, blocks nothing. Records are unioned
cluster-wide and filed back to the app repo as PRs that widen the profile.

### candidate
Discovery has flattened and the checklist (below) is satisfied. Promote on a
canary fraction of nodes, watch, continue or fall back.

### enforcing
Any `DENIED` demotes the workload — **cluster-wide and immediately**. A denial
on one node is evidence the profile is wrong for the workload, not for the node.

**Asymmetry to encode: promote slowly, demote everywhere at once.**

**Changed 2026-11-10: automatic demotion is limited to probation.** A blocked
attack and a too-narrow profile produce the same `DENIED`. If any denial
demoted forever, an attacker inside a confined pod could trigger one denial,
wait for the profile to drop to complain, and proceed. So a `DemotionPolicy`
defines a probation after each promotion, measured in observed events (deploys,
restarts, business cycles) rather than elapsed time. During probation a denial
demotes as above. After it, the default is to alert and leave demotion to a
human; demoting automatically after probation is an explicit opt-in.

## 5. Promotion gating — do not use a silence timer

This is the subtlest part of the design and the easiest to get wrong.

A profile goes quiet both when it is **complete** and when the uncovered path
simply **has not run yet**. Seven quiet days says nothing about:

- the cert renewal at day 60
- the error handler for an upstream that has not failed yet
- the quarterly job
- the retry branch that only opens under load you have not had

So gate on **observed events**, not elapsed silence. Promotion requires:

- [ ] New-rule discovery rate decayed to zero and flat across at least one
      full business cycle
- [ ] A deploy observed under this profile
- [ ] A restart observed
- [ ] A rollback observed
- [ ] An upstream timeout / error path observed
- [ ] A cert rotation observed, where applicable
- [ ] Route coverage from the app's own telemetry agrees every handler was hit
- [ ] **No dropped audit records** (§6)

Treat it as a checklist of things that happened, not a threshold that elapsed.

## 6. The failure mode that fails open

**Lost audit records look exactly like quiet.** If the kernel ring buffer
wraps, the collector falls behind, or auditd hits its backlog limit, the
silence the controller is promoting on was caused by *not looking*.

This is the one way the design fails closed-into-open, so:

- Use `auditd`/`audispd`, not `dmesg`. The ring buffer gives no way to know
  what was missed; auditd reports lost-record counts.
- A moving lost-record counter is a **hard block** on promotion, not a warning.

## 7. Distribution and loading

### The flip is cheap
Changing complain → enforce needs no pod restart and no spec change. Edit the
profile's flags, `apparmor_parser -r`, and the kernel applies the new policy to
**already-running confined processes immediately** — profiles are matched by
name. Demotion is the same operation in reverse.

That reversibility is what makes automated promotion defensible at all.

### Pods carry a stable profile name
Use `securityContext.appArmorProfile` (GA in k8s 1.30; the old
`container.apparmor.security.beta.kubernetes.io/*` annotation is deprecated).
The name stays fixed across promotions so nothing in the pod spec changes.

### Node labels must carry a content hash
Profiles load by name, so a node can hold a **stale** profile under the right
name and look healthy. `"loaded everywhere"` is unverifiable unless the label
says which revision:

```
apparmor.armadillo/<profile>: sha256-<hash>
```

This gives the controller real coverage data, lets affinity require the
*current* revision rather than mere presence, and answers the question you
will actually ask during an incident: what policy is on that node right now?

**Changed 2026-11-10: the hash lives in `ProfileNodeStatus`, not a node label.**
The invariant stands — every node reports the hash of what it loaded — but the
loader writes it to `ProfileNodeStatus.status.loadedHash`. Labelling nodes
needs `patch` on `nodes`, and a DaemonSet's service account cannot be scoped to
one node, so a single compromised node could cordon or relabel every other
one. The label's other use, affinity on the current revision, went away when
§8 chose the startup gate over affinity. An admission policy
(`config/loader/node_restriction.yaml`) limits each loader to the
`ProfileNodeStatus` objects of its own node.

### Stage the rollout even though the decision is global
A reload applies instantly to running processes — which is exactly why a
simultaneous cluster-wide enforce flip is dangerous. Decide globally, roll out
by node, pause on denials.

### Profile text is untrusted input
Creating a `ManagedProfile` means authoring kernel policy for every node. The
loader runs as root and loads whatever text the profile holds, and AppArmor
names are node-global. Raw profile text can:

- replace the runtime's default profile (`cri-containerd.apparmor.d`) or
  another namespace's profile by reusing its name
- attach to host binaries — a profile named `/usr/bin/kubelet` in enforce mode
  takes the node down
- `include` arbitrary host files, which the parser reads as root
- be an allow-everything profile, which is weaker than the runtime default yet
  still passes Pod Security "baseline" because it is a `Localhost` profile

So the loader never loads the text as written:

- It derives the profile name itself: `armadillo.<namespace>.<name>`. A
  namespace cannot contain a dot, so two namespaces can never produce the same
  name. The name in the text is ignored.
- It sets the mode flags itself. Complain or enforce is the controller's
  decision, never the file's.
- It rejects attachments, more than one profile per file, and includes outside
  an allowlist.

Create rights on `ManagedProfile` are privileged and should be granted like
the right to change node configuration, not like the right to deploy an app.

## 8. New nodes and the scheduling race

A container whose profile is not loaded on the node **fails to start**
(`CreateContainerError`) — it does not silently fall back to unconfined. Correct,
but it makes every new node a race: autoscaler adds a node, scheduler places a
pod, loader has not run yet.

Note the problem is narrower than it first appears: **failover onto existing
nodes is not gated at all**, since those nodes already hold the profile. Only
scale-out, and failover that must grow the pool, are affected.

### Chosen approach: move the wait from scheduling to startup

Rather than gating placement with `nodeAffinity`, let the pod schedule
immediately and block startup on a **native sidecar** — an `initContainer` with
`restartPolicy: Always` (stable since k8s 1.29). It starts before the main
containers and holds them until its readiness probe passes, and it keeps
running afterwards so it can re-check after a profile update.

A plain sidecar does **not** work here: sidecars run alongside the main
container and do not gate its start.

This is better than affinity on latency, because the pod is placed right away
and the application image pulls **in parallel** with the loader doing its work —
two waits overlap instead of serializing.

Injection is via the mutating admission webhook; the operator already knows
which workloads have profiles under management.

Implementation note (changed 2026-11-10): the gate asks the loader. The loader
serves `GET /v1/loaded/<profile>` on a host port, and the gate's startup probe
calls it through the node's IP. The earlier idea, a sentinel file in a hostPath
directory, would have put a hostPath volume into every managed pod, which the
"baseline" Pod Security Standard forbids.

### What not to do
Letting the main container just fail and retry does eventually converge, but
kubelet's backoff is exponential (10s, 20s, 40s…) — a couple of misses costs
more than the load ever would have.

### Per-workload escape hatch
For workloads where availability beats confinement during a scale event, allow
starting in complain (or unconfined) with the operator promoting once the
profile lands. This is a **real security gap** during the window and should be
an explicit per-workload opt-in, never a default.

## 9. Source of truth

The operator and the action must write to **the same place**: the profile
committed in the application repo. If the operator keeps its own store while
the action maintains a copy in git, they diverge and the time goes into
reconciling them instead of using them.

## 10. Open questions

- **CRD shape.** Rough sketch: a `ManagedProfile` (the profile, its lifecycle
  state, its content hash) and a `ProfileBinding` (workload → profile). Not
  settled.
- **API domain.** `aigrid.linkervision.com` matches `lmshard-operator`;
  `apparmor-profile-action` lives under personal `Tanchwa`. Unresolved — it
  bakes into every `apiVersion`.
- **Where the union lives.** In-cluster store, or recomputed from the app repo
  plus a records buffer? Affects what happens when the operator restarts.
- **How PRs get filed.** Reuse the action's propose job, or talk to the Git
  forge directly?
- **What "one full business cycle" means** per workload, and whether it is
  configurable or inferred.

## 11. Deferred

- **Bake profiles into the node image**, with the operator calling back to an
  image-builder endpoint. This is the *better* answer to §8 — a node is born
  with the profile already loaded, so node birth is not gated on a reconcile
  loop at all, and the DaemonSet becomes an updater rather than a prerequisite.
  Deliberately parked until the basics work.
- Fine-grained AppArmor network rules (`ip=`, `port=`). AppArmor 4 has them but
  kernel support is not reliably enabled; the action does not emit them either.
