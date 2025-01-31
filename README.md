# gorunner
`gorunner` is a small Go library for managing concurrent executions of typed pipelines (called InstructionSets) with retry support and type safety checks at runtime. You can define a series of functions (Instructions) that transform data of a specific type. A `Runner` orchestrates these sets, enforcing concurrency limits and automatically retrying failed executions.

## Features
* **Typed pipelines**: Each InstructionSet is bound to a concrete type, preventing accidental type mixing.
* **Concurrent execution**: Control concurrency through a semaphore mechanism.
* **Retry support**: Automatically re-run a pipeline upon failure, up to a specified limit.
* **Callbacks**: Optional `onErr` and `onSuccess` callbacks to handle outcomes.

*Disclaimer* - ChatGPT was used to write documentation and tests.