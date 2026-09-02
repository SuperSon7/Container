# Container

컨테이너를 한 문장으로 정의하면 다음과 같다.

> 컨테이너는 별도의 작은 운영체제가 아니라, 호스트 커널이 격리해서 실행하는 프로세스다.

가상 머신은 하드웨어를 가상화하고 각 VM이 자기 커널을 실행한다.
컨테이너는 호스트 커널을 공유하면서 프로세스가 보는 PID, 파일시스템, 네트워크와 사용할 수 있는 자원을 제한한다.

```txt
Virtual Machine                         Container

Application      Application            Process A       Process B
Guest OS         Guest OS               Rootfs A        Rootfs B
Guest Kernel     Guest Kernel           격리된 namespace/cgroup
Hypervisor                              Host Linux Kernel
Host Hardware                           Host Hardware
```

이 차이 때문에 컨테이너는 보통 VM보다 빠르게 시작하고 적은 자원으로 많이 실행할 수 있다.
대신 호스트 커널을 공유하므로 VM과 완전히 같은 보안 경계라고 생각해서는 안 된다.

## 컨테이너는 어떻게 생겨났는가

컨테이너는 어느 날 하나의 제품으로 갑자기 등장한 기술이 아니다.
운영체제가 프로세스를 더 안전하게 격리하려고 발전시킨 기능들이 오랜 시간 합쳐진 결과다.

1. **`chroot` — 파일시스템 경계의 시작**

   초기 Unix의 `chroot`는 프로세스가 특정 디렉터리 바깥을 보지 못하게 했다.
   하지만 파일 경로만 바꿀 뿐 프로세스, 사용자, 네트워크와 자원은 여전히 호스트와 공유했다.

2. **Jail과 Zone — 운영체제 수준 격리**

   FreeBSD Jail은 `chroot`를 확장해 프로세스와 사용자, 네트워크까지 격리했다.
   Solaris Zones도 하나의 커널 위에 여러 독립된 실행 환경을 제공했다.
   이 시기에 “하나의 커널을 공유하는 가상 환경”이라는 형태가 구체화됐다.

3. **Linux namespaces와 cgroups — 현재 컨테이너의 커널 기반**

   Linux namespaces는 프로세스마다 서로 다른 시스템의 모습을 보여준다.
   cgroups는 프로세스 그룹이 사용할 수 있는 CPU, 메모리와 PID 수를 측정하고 제한한다.
   두 기능이 결합하면서 Linux에서도 독립된 실행 환경을 만들 수 있게 됐다.

4. **Docker — 커널 기능을 제품 경험으로 연결**

   Docker는 2013년 공개된 뒤 복잡한 커널 기능을 `build`, `pull`, `run` 같은 단순한 사용 흐름으로 묶었다.
   애플리케이션과 의존성을 이미지로 만들고 registry를 통해 공유하면서 컨테이너가 개발과 배포의 실용적인 단위가 됐다.

5. **OCI와 Kubernetes — 표준화와 대규모 운영**

   2015년 출범한 OCI는 이미지 형식, 이미지 배포, 런타임 동작의 공통 규격을 만들었다.
   Kubernetes는 여러 서버에 있는 컨테이너의 배치, 복구, 확장과 네트워크 연결을 자동화했다.

정리하면 다음과 같다.

```txt
chroot
  -> OS-level isolation
  -> Linux namespaces + cgroups
  -> Docker image/registry/CLI
  -> OCI 표준
  -> Kubernetes orchestration
```

## 왜 주류가 되었는가

컨테이너가 주류가 된 이유는 단순히 “가볍기 때문”만은 아니다.

- **일관성**: 코드와 라이브러리, 실행 설정을 이미지로 묶어 개발과 운영 환경의 차이를 줄였다.
- **작은 배포 단위**: VM 전체 대신 애플리케이션과 필요한 사용자 공간만 전달한다.
- **빠른 교체**: 새 커널을 부팅하지 않고 프로세스를 시작하므로 배포, 복구와 확장에 유리하다.
- **공유 생태계**: registry에서 표준 이미지를 내려받고 digest로 같은 결과물을 식별할 수 있다.
- **자동 운영**: Kubernetes 같은 오케스트레이터가 배치, 복구, 확장과 네트워크 연결을 자동화한다.

즉, 격리 기술만으로 주류가 된 것이 아니라 이미지, registry, 표준 runtime과 orchestration이 함께 생태계를 만들었기 때문이다.

## 어떤 기술로 동작하는가

컨테이너는 하나의 커널 기능이 아니라 여러 기능을 정해진 순서로 조합한 결과다.

| 기술 | 해결하는 문제 | 컨테이너에서 보이는 결과 |
| --- | --- | --- |
| Namespaces | 무엇을 볼 수 있는가 | 독립된 PID, hostname, mount, network, IPC, user 공간 |
| cgroups | 얼마나 사용할 수 있는가 | CPU, memory, PID 수 제한과 사용량 측정 |
| rootfs와 `pivot_root` | 어떤 파일을 볼 수 있는가 | 이미지에서 준비한 `/`만 보이는 파일시스템 |
| OverlayFS | 이미지를 어떻게 효율적으로 쌓는가 | 읽기 전용 layer 공유와 컨테이너별 writable layer |
| veth와 bridge | 격리된 네트워크를 어떻게 연결하는가 | 컨테이너 `eth0`, 같은 호스트의 컨테이너 간 통신 |
| Route와 NAT | 외부 네트워크와 어떻게 통신하는가 | default gateway, IP forwarding, masquerade |
| Capabilities, seccomp, LSM | root 권한과 syscall을 어떻게 줄이는가 | 필요한 권한과 시스템 호출만 허용 |
| OCI specification | 구현체 사이의 약속을 어떻게 맞추는가 | 공통 image 형식과 runtime lifecycle |

namespace는 PID, mount, hostname, network 같은 전역 커널 자원을 감싸서 프로세스가 자기만의 시스템을 가진 것처럼 보이게 한다.
cgroups는 그 프로세스 그룹의 CPU, memory와 PID 사용량을 측정하고 제한한다.

```txt
namespace = 무엇을 볼 수 있는가
cgroup    = 얼마나 사용할 수 있는가
```

이미지는 컨테이너 실행에 필요한 파일과 설정을 담은 불변 패키지이고, 컨테이너는 그 이미지에서 실제로 실행된 프로세스다.
OverlayFS는 읽기 전용 image layer를 공유하면서 변경분만 컨테이너별 writable layer에 기록한다.

새 network namespace에는 외부 통로가 없으므로 런타임은 veth를 host bridge에 연결하고 route, IP forwarding과 NAT를 구성한다.

```txt
container process
  -> eth0
  -> veth pair
  -> Linux bridge
  -> route / NAT
  -> host NIC
  -> external network
```

저수준 runtime은 namespace, cgroup, rootfs, network와 보안 설정을 준비한 뒤 애플리케이션 프로세스를 실행한다.
OCI는 image, runtime bundle과 `create/start/state/kill/delete` lifecycle의 공통 규격을 정의한다.
Docker나 containerd 같은 상위 도구는 이미지와 컨테이너를 관리하고, runc 같은 저수준 runtime에 실제 프로세스 실행을 맡긴다.

## `docker run` 안에서 일어나는 일

세부 구현은 런타임마다 다르지만 핵심 흐름은 다음과 같다.

```txt
1. image manifest와 layer를 가져오고 digest를 검증한다
2. layer를 조립해 rootfs를 준비한다
3. namespace를 만들거나 기존 namespace에 참여한다
4. cgroup을 만들고 resource limit을 설정한다
5. veth, route, DNS, NAT와 port mapping을 구성한다
6. mount를 준비하고 rootfs를 전환한다
7. capability와 syscall 보안 정책을 적용한다
8. 컨테이너의 명령을 PID 1로 exec한다
9. 종료 시 network, cgroup, mount와 runtime state를 정리한다
```

컨테이너 런타임을 이해한다는 것은 이 흐름에서 문제가 발생했을 때 어느 계층을 관찰해야 하는지 아는 것이다.

## 이 프로젝트에서 확인하는 것

이 저장소는 위 동작을 Go와 Linux 커널 인터페이스로 직접 구현하며 학습하는 프로젝트다.

현재 구현된 범위:

- PID, UTS, Mount, Network namespace를 사용하는 parent/child 실행 구조
- cgroup v1/v2 manager와 실행 경로의 PID 제한
- Alpine rootfs를 사용하는 `chroot` 기반 파일시스템 격리
- bridge, veth, container IP, default route와 iptables MASQUERADE
- 부모가 네트워크 설정을 끝낼 때까지 자식을 막는 pipe 동기화
- 실패와 종료 시 network/cgroup 정리 흐름

다음 학습 범위:

- 공유 bridge/NAT와 컨테이너별 veth/IP의 lifecycle 분리
- 두 컨테이너 동시 실행, DNS와 port mapping
- `chroot`를 `pivot_root`로 교체하고 runtime 설정의 하드코딩 제거
- OCI bundle lifecycle과 registry image 실행 경로

이 프로젝트의 목표는 Docker를 다시 만드는 것 자체가 아니다.
컨테이너 장애가 발생했을 때 namespace, cgroup, mount, route, NAT와 image layer 중 어디를 확인해야 하는지 원리부터 이해하는 것이 목표다.

## 참고 자료

- [Docker Docs: What is a container?](https://docs.docker.com/get-started/docker-concepts/the-basics/what-is-a-container/)
- [Linux manual: namespaces(7)](https://man7.org/linux/man-pages/man7/namespaces.7.html)
- [Linux kernel: cgroup v2](https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html)
- [Linux kernel: OverlayFS](https://www.kernel.org/doc/html/latest/filesystems/overlayfs.html)
- [FreeBSD Handbook: Jails and Containers](https://docs.freebsd.org/en/books/handbook/jails/)
- [Oracle Solaris Zones](https://docs.oracle.com/cd/E19963-01/pdf/821-1460.pdf)
- [Docker: first public demo in 2013](https://www.docker.com/blog/docker-nine-years-young/)
- [Open Container Initiative](https://opencontainers.org/about/overview/)
- [Kubernetes: Containers](https://kubernetes.io/docs/concepts/containers/)
- [runc/libcontainer](https://github.com/opencontainers/runc/tree/main/libcontainer)
