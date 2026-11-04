#pragma once

#include <array>
#include <cstddef>
#include <cstdint>
#include <functional>
#include <string>

namespace ir_command {

constexpr std::size_t MAX_RAW_TIMINGS = 1024;
constexpr unsigned int MAX_RAW_TIMING_US = 100000;
constexpr unsigned long long MAX_RAW_TOTAL_US = 1000000;

struct Result {
    bool ok = false;
    std::string error_code;
    std::string protocol;
    std::string code;
    std::uint64_t code_value = 0;
    std::array<unsigned int, MAX_RAW_TIMINGS> timings{};
    std::size_t timing_count = 0;
};

using RawSendCallback = std::function<void(const unsigned int*, std::size_t)>;
using CodeSendCallback = std::function<void(const std::string&, std::uint64_t)>;

Result execute(
    const std::string& command,
    const RawSendCallback& send_raw,
    const CodeSendCallback& send_code
);

}  // namespace ir_command
