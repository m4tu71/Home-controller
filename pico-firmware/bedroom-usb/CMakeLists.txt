cmake_minimum_required(VERSION 3.13)

include($ENV{PICO_SDK_PATH}/external/pico_sdk_import.cmake)

project(bedroom_usb C CXX ASM)

set(CMAKE_C_STANDARD 11)
set(CMAKE_CXX_STANDARD 17)

pico_sdk_init()

add_executable(bedroom_usb
    main.cpp
    led_controller.cpp
    command_handler.cpp
    ir_command_parser.cpp
    ir_sender.cpp
    ir_receiver.cpp
)

target_link_libraries(bedroom_usb
    pico_stdlib
)

pico_enable_stdio_usb(bedroom_usb 1)
pico_enable_stdio_uart(bedroom_usb 0)

file(STRINGS "${CMAKE_CURRENT_LIST_DIR}/../../VERSION" WEB_CONTROLLER_VERSION LIMIT_COUNT 1)
pico_set_program_name(bedroom_usb "web-controller-pico2h")
pico_set_program_version(bedroom_usb "${WEB_CONTROLLER_VERSION}")
pico_add_extra_outputs(bedroom_usb)
