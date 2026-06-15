/*
 * demo-app.c — xcover userspace BPF demo target
 *
 * A simple C program with several distinct functions so xcover can measure
 * which ones were executed.  Build with:
 *
 *   gcc -O0 -o demo-app demo-app.c
 *
 * -O0 ensures every function gets a proper prologue that Frida-GUM can
 * intercept (avoids trivially short leaf functions).
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int add(int a, int b) {
	return a + b;
}

int multiply(int a, int b) {
	return a * b;
}

int subtract(int a, int b) {
	return a - b;
}

int divide(int a, int b) {
	if (b == 0) {
		fprintf(stderr, "division by zero\n");
		return 0;
	}
	return a / b;
}

void greet(const char *name) {
	printf("Hello, %s!\n", name);
}

int main(int argc, char *argv[]) {
	if (argc < 2) {
		fprintf(stderr, "Usage: demo-app <command>\n");
		fprintf(stderr, "Commands: add, multiply, subtract, divide, greet, all\n");
		return 1;
	}

	const char *cmd = argv[1];

	if (strcmp(cmd, "add") == 0) {
		printf("10 + 20 = %d\n", add(10, 20));
	} else if (strcmp(cmd, "multiply") == 0) {
		printf("5 * 6 = %d\n", multiply(5, 6));
	} else if (strcmp(cmd, "subtract") == 0) {
		printf("30 - 10 = %d\n", subtract(30, 10));
	} else if (strcmp(cmd, "divide") == 0) {
		printf("100 / 5 = %d\n", divide(100, 5));
	} else if (strcmp(cmd, "greet") == 0) {
		greet("xcover");
	} else if (strcmp(cmd, "all") == 0) {
		printf("Running all functions:\n");
		printf("Add:      %d\n", add(10, 20));
		printf("Multiply: %d\n", multiply(5, 6));
		printf("Subtract: %d\n", subtract(30, 10));
		printf("Divide:   %d\n", divide(100, 5));
		greet("xcover");
	} else {
		fprintf(stderr, "Unknown command: %s\n", cmd);
		return 1;
	}

	return 0;
}
