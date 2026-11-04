#include "pico/stdlib.h"

#include <cstdio>

#include "command_handler.hpp"
#include "led_controller.hpp"
#include "ir_sender.hpp"
#include "ir_receiver.hpp"

char command_buffer[4096];
int command_index = 0;

int main() {
    stdio_init_all();

    sleep_ms(1000);

    while (getchar_timeout_us(0) != PICO_ERROR_TIMEOUT) {
        // discard buffered input
    }

    led_init();
    ir_init();
    ir_receiver_init();

    while (true) {
        int ch = getchar_timeout_us(0);

        if (ch == PICO_ERROR_TIMEOUT) {
            sleep_ms(10);
            continue;
        }

        if (ch == '\r') {
            continue;
        }

        if (ch == '\n') {
            command_buffer[command_index] = '\0';

            if (command_index > 0) {
                handle_command(command_buffer);
            }

            command_index = 0;
            continue;
        }

        if (
        (ch >= 'A' && ch <= 'Z') ||
        (ch >= '0' && ch <= '9') ||
        ch == '_' ||
        ch == ':' ||
        ch == ','
    ) {
        if (command_index < static_cast<int>(sizeof(command_buffer)) - 1) {
            command_buffer[command_index] = static_cast<char>(ch);
            command_index++;
        } else {
            command_index = 0;
            printf("{\"ok\":false,\"error\":\"command too long\"}\n");
        }
    }
}   

    return 0;
}