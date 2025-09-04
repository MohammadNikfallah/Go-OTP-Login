# OTP Login API 🚀

A simple OTP-based authentication service built with **Go**, **PostgreSQL**, and **Redis**, with API docs powered by **Swagger**.  

Users can sign in with their phone number and a one-time password (OTP). Once verified, they receive a **JWT token** to access protected resources.

---

## Features
- OTP login with phone number  
- OTP stored temporarily in Redis with expiry  
- PostgreSQL for persistent user storage  
- JWT authentication (Bearer tokens)  
- Middleware for auth & panic recovery  
- Rate limiting: max 3 OTP requests per phone per 10 minutes  
- Swagger UI for API docs  

---

## Tech Stack
- Go – main backend  
- PostgreSQL – stores user records  
- Redis – caches OTPs + rate limiting  
- JWT – authentication  
- Swaggo – generates Swagger documentation  
- Docker Compose – runs Postgres + Redis locally  

---

## Why PostgreSQL + Redis?

- **PostgreSQL**: Reliable relational DB with ACID compliance. Perfect for permanent storage of users.  
- **Redis**: In-memory store, extremely fast. Ideal for temporary OTP storage and rate limiting.  

Using them together balances performance (Redis) with reliability (Postgres).

---

## Setup & Run (new PC)

1. Clone repo  
   git clone https://github.com/MohammadNikfallah/go-otp-login.git  
   cd go-otp-login  

2. Install dependencies  
   - Install Go (>= 1.20)  
   - Install Docker & Docker Compose  
   - Install Make (for Windows, use Git Bash or WSL)  
   - Install migrate CLI (for DB migrations)  
   - Install swaggo (for Swagger):  
     go install github.com/swaggo/swag/cmd/swag@latest  

3. Start services  
   make up  

4. Run migrations  
   make migrate-up  

5. Generate Swagger docs  
   make swagger  

6. Run the app  
   make run  

   Server runs on: http://localhost:8000  

7. Open Swagger UI  
   http://localhost:8000/swagger/index.html  

---

## Development
- Rebuild containers: make restart  
- Rollback DB: make migrate-down  
- Full reset: make reset  

---

## License
MIT License – free to use and modify.
