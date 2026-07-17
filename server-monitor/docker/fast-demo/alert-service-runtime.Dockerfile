ARG BASE_IMAGE=cloudops-local/alert-service:9d6a54d4687372d57a6182509bbe52dc4dc995c2
FROM ${BASE_IMAGE}

ARG VCS_REF
LABEL org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.version="fast-demo"

COPY alert-service /app/alert-service
