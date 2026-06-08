> **Docker Image** : `docker.io/advait11/aws-ecs-python-autoinstrumentation-otel:latest`


### Python FastAPI App (PORT:8000) endpoints :

> **Swagger endpoint** : `/docs`
---
```
GET     /users:id

GET     /orders

GET     /items
```

---

NOTE : For Docker.middleware.io  (if used)

- destination path is `/mnt/mw-agent/instrumentation/python/`

so env for PYTHONPATH is 
```
PYTHONPATH=/mnt/mw-agent/instrumentation/python/packages/opentelemetry/instrumentation/auto_instrumentation:/mnt/mw-agent/instrumentation/python/packages
```
