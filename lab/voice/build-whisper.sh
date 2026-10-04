#!/bin/bash
# Build whisper.cpp for the voice spike in containers (server-home):
#   build-whisper.sh cpu    Fedora 44, CPU only, every x86-64 variant picked at run
#                           time (the basalt-llm recipe), into $OUT/whisper-cpu
#   build-whisper.sh cuda   CUDA 12.6 (Pascal sm_61 and newer), into $OUT/whisper-cuda
# Fedora's whisper-cpp package is a library only (no whisper-cli or
# whisper-server) and requires the ROCm runtime (gigabytes), so the spike
# builds its own, as basalt-llm does for llama.cpp.
set -euo pipefail
VER=${WHISPER_VERSION:-1.9.4}
OUT=${OUT:-$HOME/basalt-voice/bin}
SRC=$HOME/basalt-voice/src
mkdir -p "$OUT" "$SRC"
tar=$SRC/whisper.cpp-$VER.tar.gz
[ -s "$tar" ] || curl -fsSL -o "$tar" "https://github.com/ggml-org/whisper.cpp/archive/refs/tags/v$VER.tar.gz"
sha256sum "$tar"
common="-DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=ON -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_EXAMPLES=ON -DWHISPER_SDL2=OFF -DGGML_NATIVE=OFF -DCMAKE_INSTALL_RPATH=\\\$ORIGIN/../lib -DCMAKE_BUILD_WITH_INSTALL_RPATH=ON"
case "${1:-cpu}" in
  cpu)
    podman run --rm --network slirp4netns -v "$SRC:/src:Z" -v "$OUT:/out:Z" registry.fedoraproject.org/fedora:44 bash -euc "
      dnf -q -y install cmake gcc-c++ make >/dev/null
      rm -rf /tmp/w && mkdir /tmp/w && tar -xzf /src/whisper.cpp-$VER.tar.gz -C /tmp/w --strip-components=1
      cd /tmp/w && cmake -B build $common -DGGML_BACKEND_DL=ON -DGGML_CPU_ALL_VARIANTS=ON -DCMAKE_INSTALL_PREFIX=/out/whisper-cpu >/dev/null
      cmake --build build -j\$(nproc) >/tmp/build.log 2>&1 || { tail -40 /tmp/build.log; exit 1; }
      cmake --install build >/dev/null
      cp build/bin/libggml-cpu-*.so /out/whisper-cpu/lib/ 2>/dev/null || true
      ls /out/whisper-cpu/bin /out/whisper-cpu/lib"
    ;;
  cuda)
    podman run --rm --network slirp4netns -v "$SRC:/src:Z" -v "$OUT:/out:Z" docker.io/nvidia/cuda:12.6.3-devel-ubuntu24.04 bash -euc "
      apt-get -qq update >/dev/null && apt-get -qq install -y cmake g++ make >/dev/null
      rm -rf /tmp/w && mkdir /tmp/w && tar -xzf /src/whisper.cpp-$VER.tar.gz -C /tmp/w --strip-components=1
      ln -sf /usr/local/cuda/lib64/stubs/libcuda.so /usr/local/cuda/lib64/stubs/libcuda.so.1
      export LIBRARY_PATH=/usr/local/cuda/lib64/stubs
      cd /tmp/w && cmake -B build $common -DCMAKE_EXE_LINKER_FLAGS=-Wl,-rpath-link,/usr/local/cuda/lib64/stubs -DGGML_CUDA=ON -DCMAKE_CUDA_ARCHITECTURES=61 -DGGML_AVX2=ON -DGGML_FMA=ON -DGGML_F16C=ON -DCMAKE_INSTALL_PREFIX=/out/whisper-cuda >/dev/null
      cmake --build build -j\$(nproc) >/tmp/build.log 2>&1 || { tail -40 /tmp/build.log; exit 1; }
      cmake --install build >/dev/null
      ls /out/whisper-cuda/bin /out/whisper-cuda/lib"
    ;;
esac
