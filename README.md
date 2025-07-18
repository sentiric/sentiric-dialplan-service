# Sentiric Dialplan Service

**Description:** Provides dynamic call routing decisions based on incoming call targets and defined rules for the Sentiric platform.

**Core Responsibilities:**
*   Storing and managing call routing rules (patterns, actions, parameters) persistently.
*   Evaluating incoming call destination numbers and caller context to determine the appropriate action (e.g., play announcement, route to internal user, forward to external number).
*   Providing APIs for CRUD operations on dialplan rules.
*   Utilizing its own persistent database (e.g., Redis, PostgreSQL).

**Technologies:**
*   Node.js (or Go)
*   Express/Fiber (for REST API)
*   Database connection (e.g., Redis, PostgreSQL).

**API Interactions (As an API Provider):**
*   Exposes a RESTful API for `sentiric-sip-server` to request routing decisions.
*   Exposes APIs for `sentiric-admin-ui` for dialplan CRUD operations.

**Local Development:**
1.  Clone this repository: `git clone https://github.com/sentiric/sentiric-dialplan-service.git`
2.  Navigate into the directory: `cd sentiric-dialplan-service`
3.  Install dependencies: `npm install` (Node.js) or `go mod tidy` (Go).
4.  Create a `.env` file from `.env.example` to configure database connections.
5.  Start the service: `npm start` (Node.js) or `go run main.go` (Go).

**Configuration:**
Refer to `config/` directory and `.env.example` for service-specific configurations, including database connection details and default dialplan contexts.

**Deployment:**
Designed for containerized deployment (e.g., Docker, Kubernetes). Refer to `sentiric-infrastructure`.

**Contributing:**
We welcome contributions! Please refer to the [Sentiric Governance](https://github.com/sentiric/sentiric-governance) repository for coding standards and contribution guidelines.

**License:**
This project is licensed under the [License](LICENSE).
