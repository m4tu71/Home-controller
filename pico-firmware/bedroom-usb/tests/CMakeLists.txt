cmake_minimum_required(VERSION 3.13)

project(ir_command_parser_tests LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

enable_testing()

add_executable(ir_command_parser_test
    ir_command_parser_test.cpp
    ../ir_command_parser.cpp
)

target_include_directories(ir_command_parser_test PRIVATE ..)
target_compile_options(ir_command_parser_test PRIVATE -Wall -Wextra -Werror)

add_test(NAME ir_command_parser_test COMMAND ir_command_parser_test)
