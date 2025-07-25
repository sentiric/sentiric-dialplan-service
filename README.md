# 🗺️ Sentiric Dialplan Service

**Description:** This service provides dynamic call routing decisions for the Sentiric platform. Built with **Go** for simplicity and performance, it acts as a central rule engine that determines the initial actions for an incoming call.

**Core Responsibilities:**
*   **Routing Logic:** Exposes a gRPC endpoint (`GetDialplan`) that evaluates an incoming call's destination (`to_uri`) and source (`from_uri`).
*   **Action Definition:** Returns a sequence of actions to be executed by the calling service (e.g., `ROUTE_TO_AGENT`, `REJECT`). This decouples the "decision" from the "execution".
*   **Persistent Rule Storage (Future):** While the initial version uses an in-memory map for rules, its core responsibility is to manage and query a persistent database (like PostgreSQL) for these rules.
*   **Management API (Future):** Will provide gRPC endpoints for CRUD (Create, Read, Update, Delete) operations on dialplan rules, to be used by the `sentiric-dashboard-ui`.

**Technology Stack:**
*   **Language:** Go
*   **Inter-Service Communication:**
    *   **gRPC:** Exposes a `DialplanService` for synchronous, type-safe queries from other backend services.
*   **Containerization:** Docker (Multi-stage builds for minimal, static Go binary).

**API Interactions (Server For):**
*   **`sentiric-sip-signaling-service` (gRPC):** This service calls the `GetDialplan` RPC to determine what to do with a newly received call.
*   **`sentiric-api-gateway-service` (Future gRPC):** Will call this service to manage dialplan rules from the admin dashboard.

## Getting Started

### Prerequisites
- Go (version 1.22 or later)
- Docker and Docker Compose
- Git
- All Sentiric repositories cloned into a single workspace directory.

### Local Development & Platform Setup
This service is not designed to run standalone. It is an integral part of the Sentiric platform and must be run via the central orchestrator in the `sentiric-infrastructure` repository.

1.  **Generate gRPC Code:** This service relies on gRPC code generated from `.proto` files in the `sentiric-core-interfaces` repository. You must generate this code before running the service.
    *   Navigate to the `sentiric-core-interfaces` repository.
    *   Run the make command: `make gen-go`
    *   Copy the generated `gen/dialplan/v1` folder into this project's `gen/` directory.

2.  **Configure Environment:**
    ```bash
    # Navigate to the sentiric-infrastructure directory
    cd ../sentiric-infrastructure 
    cp .env.local.example .env
    ```

3.  **Run the entire platform:** The central Docker Compose file will automatically build and run this service.
    ```bash
    # From the sentiric-infrastructure directory
    docker compose up --build -d
    ```

4.  **View Logs:**
    ```bash
    docker compose logs -f dialplan-service
    ```

## Configuration

All configuration is managed via environment variables passed from the `sentiric-infrastructure` repository's `.env` file. The primary variable for this service is `GRPC_PORT_DIALPLAN`, which defines the internal port the gRPC server will listen on.

## Deployment

This service is designed for containerized deployment. The multi-stage `Dockerfile` builds a minimal, static binary from a scratch image for maximum security and a small footprint. The CI/CD pipeline (to be created) will automatically build and push the image to the GitHub Container Registry (`ghcr.io`).

## Contributing

We welcome contributions! Please refer to the [Sentiric Governance](https://github.com/sentiric/sentiric-governance) repository for detailed coding standards, contribution guidelines, and the overall project vision.

## License

This project is licensed under the [License](LICENSE).