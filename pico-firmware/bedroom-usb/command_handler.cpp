#include "command_handler.hpp"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "pico/stdlib.h"

#include "led_controller.hpp"
#include "ir_command_parser.hpp"
#include "ir_receiver.hpp"
#include "ir_sender.hpp"

static const char* light_state = "unknown";

void send_response(const char* state) {
    printf("{\"ok\":true,\"device\":\"led\",\"state\":\"%s\"}\n", state);
}

void send_error(const char* message) {
    printf("{\"ok\":false,\"error\":\"%s\"}\n", message);
}

void send_ir_rejection(const std::string& error_code) {
    printf(
        "{\"ok\":false,\"device\":\"ir_sender\",\"error\":\"IR command rejected\",\"error_code\":\"%s\"}\n",
        error_code.c_str()
    );
}

void send_light_command(
    const char* action,
    const char* next_state,
    unsigned long long code
) {
    for (int i = 0; i < 3; i++) {
        ir_send_panasonic_40bit(code);
        sleep_ms(40);
    }

    light_state = next_state;

    printf(
        "{\"ok\":true,\"device\":\"light\",\"state\":\"%s\",\"action\":\"%s\"}\n",
        light_state,
        action
    );
}

void handle_command(const char* command) {
    if (strcmp(command, "IR_CAPABILITIES") == 0) {
        printf(
            "{\"ok\":true,\"commands\":[\"IR_SEND_RAW\",\"IR_SEND_CODE\"],"
            "\"code_protocols\":[\"PANASONIC_40\"],\"raw_carrier_khz\":[38]}\n"
        );
        return;
    }

    if (strcmp(command, "LED_ON") == 0) {
        led_on();
        send_response("on");
        return;
    }

    if (strcmp(command, "LED_OFF") == 0) {
        led_off();
        send_response("off");
        return;
    }

    if (strcmp(command, "LED_STATUS") == 0) {
        send_response(led_is_on() ? "on" : "off");
        return;
    }

    if (strcmp(command, "IR_TEST") == 0) {
        ir_test_signal();
        printf("{\"ok\":true,\"device\":\"ir\",\"state\":\"test_sent\"}\n");
        return;
        
    }

    if (strcmp(command, "LIGHT_ON_CH3") == 0) {
        send_light_command("on", "on", 0x344A90BC2CULL);
        return;
    }

    if (strcmp(command, "LIGHT_FULL_CH3") == 0) {
        send_light_command("full", "full", 0x344A903CACULL);
        return;
    }

    if (strcmp(command, "LIGHT_OFF_CH3") == 0) {
        send_light_command("off", "off", 0x344A90FC6CULL);
        return;
    }
    
    if (strcmp(command, "LIGHT_NIGHT_CH3") == 0) {
        send_light_command("night", "night", 0x344A907CECULL);
        return;
    }

    if (strcmp(command, "LIGHT_BRIGHT_UP_CH3") == 0) {
        send_light_command("bright_up", light_state, 0x344A905CCCULL);
        return;
    }

    if (strcmp(command, "LIGHT_BRIGHT_DOWN_CH3") == 0) {
        const char* next_state = light_state;

        if (strcmp(light_state, "full") == 0) {
            next_state = "on";
        }

        send_light_command("bright_down", next_state, 0x344A90DC4CULL);
        return;
    }

    if (strcmp(command, "LIGHT_STATUS_CH3") == 0) {
        printf("{\"ok\":true,\"device\":\"light\",\"state\":\"%s\"}\n", light_state);
        return;
    }


    if (strcmp(command, "IR_DETECT_TEST") == 0) {
        bool detected = ir_wait_for_signal_ms(5000);
    
        if (detected) {
            led_on();
            printf("{\"ok\":true,\"device\":\"ir_receiver\",\"state\":\"detected\"}\n");
        } else {
            printf("{\"ok\":false,\"device\":\"ir_receiver\",\"error\":\"timeout\"}\n");
        }

        return;
    }

    if (strcmp(command, "IR_CAPTURE") == 0) {
        const int MAX_TIMINGS = 1024;
        unsigned int timings[MAX_TIMINGS];

        int count = ir_capture_signal(
            timings,
            MAX_TIMINGS,
            5000,   // 信号開始待ち: 5秒
            80000   // 80ms無信号なら終了
        );

        if (count == 0) {
            printf("{\"ok\":false,\"device\":\"ir_receiver\",\"error\":\"timeout\"}\n");
            return;
        }

        printf(
            "{\"ok\":true,\"device\":\"ir_receiver\",\"state\":\"captured\",\"count\":%d,\"timings_us\":[",
            count
        );

        for (int i = 0; i < count; i++) {
            if (i > 0) {
                printf(",");
            }

            printf("%u", timings[i]);
        }

        printf("]}\n");
        return;
    }

    if (strncmp(command, "IR_SEND_", 8) == 0) {
        const ir_command::Result result = ir_command::execute(
            command,
            [](const unsigned int* timings, std::size_t count) {
                ir_send_raw_timings(timings, static_cast<int>(count));
            },
            [](const std::string&, std::uint64_t code) {
                ir_send_panasonic_40bit(code);
            }
        );

        if (!result.ok) {
            send_ir_rejection(result.error_code);
            return;
        }
        if (result.timing_count > 0) {
            printf(
                "{\"ok\":true,\"device\":\"ir_sender\",\"state\":\"raw_sent\",\"count\":%u}\n",
                static_cast<unsigned int>(result.timing_count)
            );
        } else {
            printf(
                "{\"ok\":true,\"device\":\"ir_sender\",\"state\":\"code_sent\",\"protocol\":\"%s\"}\n",
                result.protocol.c_str()
            );
        }
        return;
    }

    send_error("unknown command");
}
