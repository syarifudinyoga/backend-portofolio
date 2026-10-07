pipeline {
  agent any

  options {
    disableConcurrentBuilds()
    timeout(time: 30, unit: 'MINUTES')
  }

  parameters {
    string(
      name: 'VERSION',
      defaultValue: 'latest',
      description: 'Image tag version (e.g. latest, v1.0.0, etc.)',
      trim: true
    )
    string(
      name: 'GHCR_NAMESPACE',
      defaultValue: 'syarifudinyoga',
      description: 'Lowercase GitHub user or organization that owns the GHCR package',
      trim: true
    )
  }

  environment {
    VPS_HOST = '100.87.90.67'
    VPS_USER = 'devlabhub'
    VPS_DIR  = '/home/devlabhub/portfolio-backend'
    GIT_REPO = 'https://github.com/syarifudinyoga/backend-portofolio.git'

    SSH_CREDENTIAL  = 'biznet-vps'
    ENV_CREDENTIAL  = 'portfolio-backend-env'
    GHCR_CREDENTIAL = 'ghcr-write'

    PODMAN         = 'podman'
    PODMAN_COMPOSE = '~/.local/bin/podman-compose'
    BACKEND_IMAGE  = 'portfolio-backend'
    BUILDX_BUILDER = 'portfolio-backend-builder'
  }

  stages {

    stage('Validate Inputs') {
      steps {
        script {
          if (!(params.VERSION ==~ /^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/)) {
            error('VERSION must be a safe container image tag.')
          }
          if (!(params.GHCR_NAMESPACE ==~ /^[a-z0-9][a-z0-9-]{0,38}$/)) {
            error('GHCR_NAMESPACE must be a lowercase GitHub user or organization name.')
          }
        }
      }
    }

    stage('Checkout SCM') {
      steps {
        checkout scm
      }
    }

    stage('Build and Push Image to GHCR') {
      steps {
        withCredentials([
          usernamePassword(
            credentialsId: env.GHCR_CREDENTIAL,
            usernameVariable: 'GHCR_USER',
            passwordVariable: 'GHCR_TOKEN'
          )
        ]) {
          sh '''
            set +x
            export PATH="/Applications/Docker.app/Contents/Resources/bin:$PATH"
            printf '%s' "$GHCR_TOKEN" | docker login ghcr.io --username "$GHCR_USER" --password-stdin
            trap 'docker logout ghcr.io >/dev/null 2>&1 || true' EXIT

            docker buildx inspect "$BUILDX_BUILDER" >/dev/null 2>&1 || \
              docker buildx create --name "$BUILDX_BUILDER" --driver docker-container
            docker buildx inspect "$BUILDX_BUILDER" --bootstrap

            docker buildx build \
              --builder "$BUILDX_BUILDER" \
              --platform linux/amd64,linux/arm64 \
              --push \
              --tag "ghcr.io/${GHCR_NAMESPACE}/${BACKEND_IMAGE}:${VERSION}" \
              --file Containerfile \
              .
          '''
        }
      }
    }

    stage('Test Homeserver Connection') {
      steps {
        sshagent([env.SSH_CREDENTIAL]) {
          sh '''
            echo "Testing SSH connection to homeserver ${VPS_HOST}..."
            ssh -o StrictHostKeyChecking=yes \
              ${VPS_USER}@${VPS_HOST} \
              "echo '=== Homeserver Info ===' && hostname && whoami && \
               echo '=== Podman Version ===' && ${PODMAN} --version && \
               echo '=== Podman Compose Version ===' && ${PODMAN_COMPOSE} version"
          '''
        }
      }
    }

    stage('Sync Git on Homeserver') {
      steps {
        sshagent([env.SSH_CREDENTIAL]) {
          sh '''
            ssh -o StrictHostKeyChecking=yes ${VPS_USER}@${VPS_HOST} "bash -s" <<REMOTE
set -eu
if [ ! -d "${VPS_DIR}/.git" ]; then
  echo "Cloning repository on homeserver..."
  mkdir -p "\$(dirname "${VPS_DIR}")"
  git clone "${GIT_REPO}" "${VPS_DIR}"
  cd "${VPS_DIR}"
else
  echo "Pulling latest changes from Git on homeserver..."
  cd "${VPS_DIR}"
  git fetch origin
  git reset --hard origin/main || git pull origin main
fi
echo "Git workspace synced: \$(git rev-parse --short HEAD)"
REMOTE
          '''
        }
      }
    }

    stage('Deploy Environment') {
      steps {
        withCredentials([
          file(
            credentialsId: env.ENV_CREDENTIAL,
            variable: 'SECRET_ENV_FILE'
          )
        ]) {
          sshagent([env.SSH_CREDENTIAL]) {
            sh '''
              ssh -o StrictHostKeyChecking=yes \
                ${VPS_USER}@${VPS_HOST} \
                "rm -f ${VPS_DIR}/.env.new"

              scp -o StrictHostKeyChecking=yes \
                "$SECRET_ENV_FILE" \
                ${VPS_USER}@${VPS_HOST}:${VPS_DIR}/.env.new

              ssh -o StrictHostKeyChecking=yes \
                ${VPS_USER}@${VPS_HOST} \
                "chmod 600 ${VPS_DIR}/.env.new && \
                 mv -f ${VPS_DIR}/.env.new ${VPS_DIR}/.env && \
                 echo 'Environment file successfully configured.'"
            '''
          }
        }
      }
    }

    stage('Prepare Network') {
      steps {
        sshagent([env.SSH_CREDENTIAL]) {
          sh '''
            ssh -o StrictHostKeyChecking=yes ${VPS_USER}@${VPS_HOST} 'bash -s' <<'REMOTE'
set -eu
NETWORK=portfolio_default
CONF="$HOME/.config/cni/net.d/${NETWORK}.conflist"
BACKEND=$(podman info --format '{{.Host.NetworkBackend}}' 2>/dev/null || echo "cni")

if ! podman network exists "$NETWORK" 2>/dev/null; then
  podman network create "$NETWORK" >/dev/null
  echo "Network $NETWORK created."
fi

# CNI patch for Podman 3.x if CNI conflist exists
if [ "$BACKEND" = "cni" ] && [ -f "$CONF" ]; then
  if grep -q '"cniVersion": "1.0.0"' "$CONF"; then
    sed -i 's/"cniVersion": "1.0.0"/"cniVersion": "0.4.0"/' "$CONF"
    echo "Patched $NETWORK cniVersion to 0.4.0."
  fi
  grep -E 'cniVersion|dnsname' "$CONF" 2>/dev/null || true
fi

echo "Network $NETWORK is valid and ready."
REMOTE
          '''
        }
      }
    }

    stage('Start Infrastructure') {
      steps {
        sshagent([env.SSH_CREDENTIAL]) {
          sh '''
            ssh -o StrictHostKeyChecking=yes \
              ${VPS_USER}@${VPS_HOST} \
              "cd ${VPS_DIR} && GHCR_NAMESPACE=${GHCR_NAMESPACE} VERSION=${VERSION} ${PODMAN_COMPOSE} up -d postgres minio"
          '''
        }
      }
    }

    stage('Wait for PostgreSQL') {
      steps {
        sshagent([env.SSH_CREDENTIAL]) {
          sh '''
            ssh -o StrictHostKeyChecking=yes ${VPS_USER}@${VPS_HOST} 'bash -s' <<'REMOTE'
set -u
echo "Waiting for PostgreSQL container..."
for i in $(seq 1 30); do
  if podman exec portfolio-postgres pg_isready -U postgres >/dev/null 2>&1 || \
     podman exec postgres pg_isready -U postgres >/dev/null 2>&1; then
    echo "PostgreSQL is ready."
    exit 0
  fi
  echo "Waiting for PostgreSQL (attempt $i/30)..."
  sleep 2
done
echo "PostgreSQL did not become ready after 60 seconds"
exit 1
REMOTE
          '''
        }
      }
    }

    stage('Wait for MinIO') {
      steps {
        sshagent([env.SSH_CREDENTIAL]) {
          sh '''
            ssh -o StrictHostKeyChecking=yes ${VPS_USER}@${VPS_HOST} 'bash -s' <<'REMOTE'
set -u
echo "Waiting for MinIO container..."
for i in $(seq 1 30); do
  if curl --silent --fail http://127.0.0.1:9000/minio/health/ready >/dev/null 2>&1; then
    echo "MinIO is ready."
    exit 0
  fi
  echo "Waiting for MinIO (attempt $i/30)..."
  sleep 2
done
echo "MinIO did not become ready after 60 seconds"
exit 1
REMOTE
          '''
        }
      }
    }

    stage('Pull Image on Homeserver') {
      steps {
        withCredentials([
          usernamePassword(
            credentialsId: env.GHCR_CREDENTIAL,
            usernameVariable: 'GHCR_USER',
            passwordVariable: 'GHCR_TOKEN'
          )
        ]) {
          sshagent([env.SSH_CREDENTIAL]) {
            sh '''
              set +x
              case "$GHCR_USER" in
                ''|*[!A-Za-z0-9-]*)
                  echo "GHCR credential username contains unsupported characters." >&2
                  exit 1
                  ;;
              esac

              REMOTE_SCRIPT='set -eu
              IFS= read -r TOKEN
              printf "%s" "$TOKEN" | podman login ghcr.io --username "$1" --password-stdin
              trap "podman logout ghcr.io >/dev/null 2>&1 || true" EXIT
              cd "$2"
              GHCR_NAMESPACE="$3" VERSION="$4" "$HOME/.local/bin/podman-compose" pull api'

              printf '%s\n' "$GHCR_TOKEN" | ssh -T -o StrictHostKeyChecking=yes ${VPS_USER}@${VPS_HOST} "sh -c '$REMOTE_SCRIPT' sh $GHCR_USER $VPS_DIR $GHCR_NAMESPACE $VERSION"
            '''
          }
        }
      }
    }

    stage('Deploy API') {
      steps {
        sshagent([env.SSH_CREDENTIAL]) {
          sh '''
            ssh -o StrictHostKeyChecking=yes \
              ${VPS_USER}@${VPS_HOST} \
              "cd ${VPS_DIR} && GHCR_NAMESPACE=${GHCR_NAMESPACE} VERSION=${VERSION} ${PODMAN_COMPOSE} up -d --no-deps --force-recreate api"
          '''
        }
      }
    }

    stage('Health Check') {
      steps {
        sshagent([env.SSH_CREDENTIAL]) {
          sh '''
            ssh -o StrictHostKeyChecking=yes ${VPS_USER}@${VPS_HOST} 'bash -s' <<'REMOTE'
set -u
echo "Verifying API health status..."
for i in $(seq 1 30); do
  STATUS=$(curl --silent --output /dev/null --write-out '%{http_code}' http://127.0.0.1:3004/api/health || true)
  if [ "$STATUS" = "200" ] || [ "$STATUS" = "204" ]; then
    echo "API health check passed (attempt $i/30, status $STATUS)"
    exit 0
  fi
  echo "Attempt $i/30: HTTP status $STATUS. Retrying in 2 seconds..."
  sleep 2
done
echo "API health check failed after 60 seconds"
podman logs --tail 50 portfolio-api 2>/dev/null || true
exit 1
REMOTE
          '''
        }
      }
    }

    stage('Prune Dangling Images') {
      steps {
        sshagent([env.SSH_CREDENTIAL]) {
          sh '''
            ssh -o StrictHostKeyChecking=yes \
              ${VPS_USER}@${VPS_HOST} \
              "${PODMAN} image prune -f"
          '''
        }
      }
    }
  }

  post {
    always {
      sh '''
        export PATH="/Applications/Docker.app/Contents/Resources/bin:$PATH"
        docker image prune -f >/dev/null 2>&1 || true
      '''
    }
    success {
      echo '''
======================================
PORTFOLIO BACKEND DEPLOYMENT SUCCESS
======================================
'''
    }
    failure {
      echo '''
======================================
PORTFOLIO BACKEND DEPLOYMENT FAILED
======================================
Check the Jenkins log output above.
'''
    }
  }
}
