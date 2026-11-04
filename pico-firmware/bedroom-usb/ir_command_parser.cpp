#include "ir_command_parser.hpp"

#include <cerrno>
#include <climits>
#include <cstdlib>

namespace ir_command {
namespace {

constexpr const char* RAW_PREFIX = "IR_SEND_RAW:";
constexpr const char* CODE_PREFIX = "IR_SEND_CODE:";
constexpr const char* PANASONIC_40 = "PANASONIC_40";
constexpr std::size_t PANASONIC_40_CODE_LENGTH = 10;

Result reject(const char* error_code) {
    Result result;
    result.error_code = error_code;
    return result;
}

bool starts_with(const std::string& value, const char* prefix) {
    return value.compare(0, std::char_traits<char>::length(prefix), prefix) == 0;
}

bool is_decimal(const std::string& value) {
    if (value.empty()) {
        return false;
    }
    for (const char character : value) {
        if (character < '0' || character > '9') {
            return false;
        }
    }
    return true;
}

bool is_upper_hex(const std::string& value) {
    for (const char character : value) {
        const bool digit = character >= '0' && character <= '9';
        const bool upper_hex = character >= 'A' && character <= 'F';
        if (!digit && !upper_hex) {
            return false;
        }
    }
    return true;
}

std::uint64_t parse_upper_hex(const std::string& value) {
    std::uint64_t parsed = 0;
    for (const char character : value) {
        const unsigned int digit = character <= '9'
            ? static_cast<unsigned int>(character - '0')
            : static_cast<unsigned int>(character - 'A' + 10);
        parsed = (parsed << 4U) | digit;
    }
    return parsed;
}

Result execute_raw(const std::string& payload, const RawSendCallback& send_raw) {
    if (payload.empty()) {
        return reject("empty_raw_timings");
    }
    if (payload.front() == ',' || payload.back() == ',') {
        return reject("invalid_raw_separator");
    }

    Result result;
    unsigned long long total = 0;
    std::size_t start = 0;
    while (start < payload.size()) {
        const std::size_t separator = payload.find(',', start);
        const std::size_t end = separator == std::string::npos ? payload.size() : separator;
        if (end == start) {
            return reject("invalid_raw_separator");
        }
        if (result.timing_count >= MAX_RAW_TIMINGS) {
            return reject("too_many_raw_timings");
        }

        const std::string token = payload.substr(start, end - start);
        if (!is_decimal(token)) {
            return reject("invalid_raw_timing");
        }

        errno = 0;
        char* parsed_end = nullptr;
        const unsigned long value = std::strtoul(token.c_str(), &parsed_end, 10);
        if (errno == ERANGE || parsed_end == token.c_str() || *parsed_end != '\0' || value > UINT_MAX || value == 0) {
            return reject("invalid_raw_timing");
        }
        if (value > MAX_RAW_TIMING_US) {
            return reject("raw_timing_exceeds_maximum");
        }
        if (total > MAX_RAW_TOTAL_US - value) {
            return reject("raw_duration_exceeds_maximum");
        }

        result.timings[result.timing_count++] = static_cast<unsigned int>(value);
        total += value;
        if (separator == std::string::npos) {
            break;
        }
        start = separator + 1;
    }

    send_raw(result.timings.data(), result.timing_count);
    result.ok = true;
    return result;
}

Result execute_code(const std::string& payload, const CodeSendCallback& send_code) {
    const std::size_t separator = payload.find(':');
    if (separator == std::string::npos || payload.find(':', separator + 1) != std::string::npos) {
        return reject("invalid_code");
    }

    Result result;
    result.protocol = payload.substr(0, separator);
    result.code = payload.substr(separator + 1);
    if (result.protocol != PANASONIC_40) {
        return reject("unsupported_protocol");
    }
    if (result.code.size() != PANASONIC_40_CODE_LENGTH || !is_upper_hex(result.code)) {
        return reject("invalid_code");
    }

    result.code_value = parse_upper_hex(result.code);
    send_code(result.protocol, result.code_value);
    result.ok = true;
    return result;
}

}  // namespace

Result execute(
    const std::string& command,
    const RawSendCallback& send_raw,
    const CodeSendCallback& send_code
) {
    if (starts_with(command, RAW_PREFIX)) {
        return execute_raw(command.substr(std::char_traits<char>::length(RAW_PREFIX)), send_raw);
    }
    if (starts_with(command, CODE_PREFIX)) {
        return execute_code(command.substr(std::char_traits<char>::length(CODE_PREFIX)), send_code);
    }
    return reject("unsupported_command");
}

}  // namespace ir_command
