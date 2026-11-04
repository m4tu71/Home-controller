#include "ir_command_parser.hpp"

#include <climits>
#include <iostream>
#include <string>
#include <vector>

namespace {

int failures = 0;

void check(bool condition, const std::string& message) {
    if (!condition) {
        std::cerr << "FAIL: " << message << '\n';
        ++failures;
    }
}

std::string repeated_timings(std::size_t count, unsigned int value) {
    std::string payload;
    for (std::size_t index = 0; index < count; ++index) {
        if (index != 0) {
            payload += ',';
        }
        payload += std::to_string(value);
    }
    return "IR_SEND_RAW:" + payload;
}

void expect_raw_success(const std::string& name, const std::string& command, std::size_t expected_count) {
    int raw_calls = 0;
    int code_calls = 0;
    const auto result = ir_command::execute(
        command,
        [&](const unsigned int*, std::size_t count) {
            ++raw_calls;
            check(count == expected_count, name + " callback count argument");
        },
        [&](const std::string&, std::uint64_t) { ++code_calls; }
    );
    check(result.ok, name + " result must be ok");
    check(result.error_code.empty(), name + " error_code must be empty");
    check(result.timing_count == expected_count, name + " parsed timing count");
    check(raw_calls == 1, name + " raw callback must run once");
    check(code_calls == 0, name + " code callback must not run");
}

void expect_rejection(const std::string& name, const std::string& command, const std::string& error_code) {
    int raw_calls = 0;
    int code_calls = 0;
    const auto result = ir_command::execute(
        command,
        [&](const unsigned int*, std::size_t) { ++raw_calls; },
        [&](const std::string&, std::uint64_t) { ++code_calls; }
    );
    check(!result.ok, name + " result must be rejected");
    check(result.error_code == error_code, name + " error_code: " + result.error_code);
    check(raw_calls == 0, name + " raw callback must not run");
    check(code_calls == 0, name + " code callback must not run");
}

void test_valid_raw_boundaries() {
    expect_raw_success("single timing maximum", "IR_SEND_RAW:100000", 1);
    expect_raw_success("total duration maximum", repeated_timings(10, 100000), 10);
    expect_raw_success("timing count maximum", repeated_timings(1024, 1), 1024);
}

void test_invalid_raw_boundaries() {
    expect_rejection("too many timings", repeated_timings(1025, 1), "too_many_raw_timings");
    expect_rejection("timing above maximum", "IR_SEND_RAW:100001", "raw_timing_exceeds_maximum");
    expect_rejection("duration above maximum", "IR_SEND_RAW:100000,100000,100000,100000,100000,100000,100000,100000,100000,100000,1", "raw_duration_exceeds_maximum");
    expect_rejection("strtoul range", "IR_SEND_RAW:999999999999999999999999999999999999999999", "invalid_raw_timing");
    expect_rejection("above unsigned int", "IR_SEND_RAW:" + std::to_string(static_cast<unsigned long long>(UINT_MAX) + 1ULL), "invalid_raw_timing");
    expect_rejection("zero", "IR_SEND_RAW:0", "invalid_raw_timing");
    expect_rejection("negative", "IR_SEND_RAW:-1", "invalid_raw_timing");
    expect_rejection("non numeric", "IR_SEND_RAW:abc", "invalid_raw_timing");
    expect_rejection("leading comma", "IR_SEND_RAW:,100", "invalid_raw_separator");
    expect_rejection("trailing comma", "IR_SEND_RAW:100,", "invalid_raw_separator");
    expect_rejection("consecutive commas", "IR_SEND_RAW:100,,200", "invalid_raw_separator");
    expect_rejection("empty timings", "IR_SEND_RAW:", "empty_raw_timings");
}

void test_code_and_command() {
    int raw_calls = 0;
    int code_calls = 0;
    std::string protocol;
    std::uint64_t code_value = 0;
    const auto result = ir_command::execute(
        "IR_SEND_CODE:PANASONIC_40:344A90BC2C",
        [&](const unsigned int*, std::size_t) { ++raw_calls; },
        [&](const std::string& parsed_protocol, std::uint64_t parsed_code) {
            ++code_calls;
            protocol = parsed_protocol;
            code_value = parsed_code;
        }
    );
    check(result.ok, "valid code result must be ok");
    check(result.protocol == "PANASONIC_40" && protocol == result.protocol, "valid code protocol");
    check(result.code == "344A90BC2C", "valid code text");
    check(result.code_value == 0x344A90BC2CULL && code_value == result.code_value, "valid code value");
    check(raw_calls == 0, "valid code raw callback must not run");
    check(code_calls == 1, "valid code callback must run once");

    expect_rejection("unsupported protocol", "IR_SEND_CODE:NEC:344A90BC2C", "unsupported_protocol");
    expect_rejection("short code", "IR_SEND_CODE:PANASONIC_40:344A90BC2", "invalid_code");
    expect_rejection("lowercase code", "IR_SEND_CODE:PANASONIC_40:344a90bc2c", "invalid_code");
    expect_rejection("non hex code", "IR_SEND_CODE:PANASONIC_40:344A90BC2Z", "invalid_code");
    expect_rejection("extra code separator", "IR_SEND_CODE:PANASONIC_40:344A90BC2C:EXTRA", "invalid_code");
    expect_rejection("unsupported command", "IR_CAPABILITIES", "unsupported_command");
}

}  // namespace

int main() {
    test_valid_raw_boundaries();
    test_invalid_raw_boundaries();
    test_code_and_command();

    if (failures != 0) {
        std::cerr << failures << " test assertion(s) failed\n";
        return 1;
    }
    std::cout << "all ir_command_parser tests passed\n";
    return 0;
}
