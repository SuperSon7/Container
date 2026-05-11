# Container

컨테이너 런타임을 처음부터 직접 구현하는 프로젝트.

`docker run`이 내부적으로 무엇을 하는지 이해하고 싶어서 시작했다.

---

## 왜 만드는가

컨테이너를 쓰는 것과 컨테이너가 어떻게 동작하는지 아는 것은 다르다.
네트워크가 안 될 때 iptables를 볼 생각을 못 하고, OOM Kill이 났을 때 cgroup을 확인할 생각을 못 하는 건 내부 구조를 모르기 때문이다.

블로그 글 읽는 걸로는 한계가 있었다. 직접 만들어봐야 "검색해서 찾는 지식"이 아니라 "당연히 아는 구조"가 된다고 생각했다.

---

## 구현 범위

### Phase 0 — 커널 인터페이스 직접 만져보기

코드 짜기 전에 손으로 먼저 해봐야 에러가 났을 때 커널이 거부하는 건지 내 코드가 틀린 건지 구분할 수 있다.

- **Linux Namespaces** — PID, UTS, Mount, IPC 격리를 `unshare`로 직접 실험
- **cgroup v2** — `memory.max`, `pids.max`, `cpu.max`를 직접 조작해서 리소스 제한 확인
- **OverlayFS** — lower/upper/merged 구조를 손으로 마운트, Copy-on-Write 동작 확인
- **pivot_root** — `chroot`와의 차이, 왜 실제 런타임이 `pivot_root`를 쓰는지

### Phase 1 — 최소 컨테이너 구현 (Go)

`syscall` 패키지로 커널 인터페이스를 직접 호출한다.

- `CLONE_NEWPID | CLONE_NEWUTS | CLONE_NEWNS`로 namespace 격리
- Alpine rootfs를 `pivot_root`로 전환
- cgroup v2로 메모리/PID 제한 적용

### Phase 2 — 네트워크 스택

이 단계가 끝나야 비로소 "서비스를 실행할 수 있는 컨테이너"가 된다.

- veth pair로 컨테이너와 호스트 연결
- Linux bridge로 컨테이너 간 통신
- iptables MASQUERADE로 외부 통신 (이게 AWS NAT Gateway가 하는 일과 원리가 같다)

### Phase 3 — 이미지 시스템 & OCI 호환

- Docker Registry HTTP API v2로 이미지 직접 pull
- OverlayFS로 레이어 조립
- [OCI Runtime Spec](https://github.com/opencontainers/runtime-spec) 구현 — `create`, `start`, `kill`, `delete`

---

## 환경

WSL2 (Ubuntu, kernel 5.15+)를 메인으로 사용한다. cgroup v2, overlayfs, namespace 모두 동작한다.
네트워크 단계에서 WSL2 NAT와 충돌하는 부분은 EC2에서 검증한다.

---

## 참고

막히면 [runc](https://github.com/opencontainers/runc/tree/main/libcontainer) 소스를 읽는다.
Liz Rice의 [Containers From Scratch](https://github.com/lizrice/containers-from-scratch)가 Phase 1의 출발점이 됐다.
