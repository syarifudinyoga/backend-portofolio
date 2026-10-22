pipeline {
  agent any

  options {
    disableConcurrentBuilds()
    timeout(time: 30, unit: 'MINUTES')
  }

  parameters {
    string(
      name: 'BRANCH',
      defaultValue: 'devlabhub',
      description: 'Git branch to deploy',
      trim: true
    )
    booleanParam(
      name: 'RUN_TESTS',
      defaultValue: true,
      description: 'Run Go Unit Test & Coverage on Homeserver'
    )
    booleanParam(
      name: 'RUN_SONAR',
      defaultValue: true,
      description: 'Run SonarQube Scanner'
    )
  }

  environment {
    // ==========================================
    // Homeserver Target Infrastructure
    // ==========================================
    SERVER_HOST      = '100.87.90.67'
    SERVER_USER      = 'devlabhub'
    SSH_USER         = 'devlabhub'
    SSH_CREDENTIAL   = 'homeserver-ssh'
    SSH_OPTS         = '-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null'

    // ==========================================
    // Application & Deployment Configuration
    // ==========================================
    APP_NAME         = 'portfolio-backend'
    CONTAINER_NAME   = 'portfolio-backend'
    IMAGE_NAME       = 'portfolio-backend'
    DEPLOY_DIR       = '/home/devlabhub/projects/portfolio-backend'
    APP_PORT         = '3004'
    HEALTH_ENDPOINT  = '/api/health'

    // ==========================================
    // Secrets & SonarQube Configuration
    // ==========================================
    ENV_CREDENTIAL   = 'portfolio-backend-env'
    SONAR_CREDENTIAL = 'sonar-devlabhub'
    SONAR_HOST_URL   = 'http://127.0.0.1:9002'

    // Mapping params to environment to avoid shell bad substitution
    OPT_BRANCH       = "${params.BRANCH}"
    OPT_RUN_TESTS    = "${params.RUN_TESTS}"
    OPT_RUN_SONAR    = "${params.RUN_SONAR}"
  }

  stages {

    stage('Checkout SCM') {
      steps {
        checkout scm
      }
    }

    stage('Test Homeserver Connection') {
      steps {
        sshagent([env.SSH_CREDENTIAL]) {
          sh '''
            echo "Checking SSH connection to ${SERVER_USER}@${SERVER_HOST}..."
            ssh ${SSH_OPTS} ${SERVER_USER}@${SERVER_HOST} "
              echo '=== Homeserver Host Info ==='
              hostname && whoami
              echo '=== Podman Info ==='
              podman --version
            "
          '''
        }
      }
    }

    stage('Sync Code to Homeserver') {
      steps {
        sshagent([env.SSH_CREDENTIAL]) {
          sh '''
            echo "Syncing code to ${SERVER_HOST}:${DEPLOY_DIR} via tar stream..."
            ssh ${SSH_OPTS} ${SERVER_USER}@${SERVER_HOST} "mkdir -p ${DEPLOY_DIR}"
            tar --exclude=.git \
                --exclude=bin \
                --exclude=vendor \
                --exclude=*.log \
                --exclude=coverage.out \
                --exclude=.scannerwork \
                -czf - . | ssh ${SSH_OPTS} ${SERVER_USER}@${SERVER_HOST} "tar -xzf - -C ${DEPLOY_DIR}"
            echo "Code successfully synchronized to ${DEPLOY_DIR}"
          '''
        }
      }
    }

    stage('Unit Test & Coverage (Podman)') {
      when {
        expression { env.OPT_RUN_TESTS == 'true' }
      }
      steps {
        sshagent([env.SSH_CREDENTIAL]) {
          sh '''
            echo "Running Unit Tests & generating coverage on Homeserver..."
            ssh ${SSH_OPTS} ${SERVER_USER}@${SERVER_HOST} "
              cd ${DEPLOY_DIR}
              podman run --rm \
                -v ${DEPLOY_DIR}:/app:Z \
                -w /app \
                docker.io/library/golang:alpine \
                sh -c 'go test -v -coverprofile=coverage.out ./...'
            "

            echo "Retrieving coverage.out back to Jenkins workspace..."
            ssh ${SSH_OPTS} ${SERVER_USER}@${SERVER_HOST} "cat ${DEPLOY_DIR}/coverage.out 2>/dev/null" > coverage.out || true

            if [ -s coverage.out ]; then
              echo "coverage.out successfully fetched (\$(wc -l < coverage.out) lines)."
            else
              echo "WARNING: coverage.out is empty or not found."
            fi
          '''
        }
      }
    }

    stage('SonarQube Analysis') {
      when {
        expression { env.OPT_RUN_SONAR == 'true' }
      }
      steps {
        withCredentials([string(credentialsId: env.SONAR_CREDENTIAL, variable: 'SONAR_TOKEN')]) {
          sshagent([env.SSH_CREDENTIAL]) {
            sh '''
              set +x
              echo "Executing SonarScanner on Homeserver via Podman..."
              ssh ${SSH_OPTS} ${SERVER_USER}@${SERVER_HOST} "
                cd ${DEPLOY_DIR}
                podman run --rm \
                  --network host \
                  -v ${DEPLOY_DIR}:/usr/src:Z \
                  -w /usr/src \
                  docker.io/sonarsource/sonar-scanner-cli \
                  sonar-scanner \
                  -Dsonar.token='${SONAR_TOKEN}' \
                  -Dsonar.host.url='${SONAR_HOST_URL}' \
                  -Dsonar.ws.timeout=300 \
                  -Dsonar.working.directory=/usr/src/.scannerwork
              "

              echo "Pulling .scannerwork report back to Jenkins workspace for UI integration..."
              ssh ${SSH_OPTS} ${SSH_USER}@${SERVER_HOST} "tar -czf - -C ${DEPLOY_DIR} .scannerwork 2>/dev/null" | tar -xzf - 2>/dev/null || true

              if [ -f .scannerwork/report-task.txt ]; then
                echo "SonarQube report-task.txt retrieved successfully."
                cat .scannerwork/report-task.txt
              else
                echo "WARNING: .scannerwork/report-task.txt not found."
              fi
            '''
          }
        }
      }
    }

    stage('Transfer Secret Environment') {
      steps {
        withCredentials([file(credentialsId: env.ENV_CREDENTIAL, variable: 'SECRET_ENV_FILE')]) {
          sshagent([env.SSH_CREDENTIAL]) {
            sh '''
              echo "Transferring .env secret to ${DEPLOY_DIR}..."
              ssh ${SSH_OPTS} ${SERVER_USER}@${SERVER_HOST} "rm -f ${DEPLOY_DIR}/.env"
              scp ${SSH_OPTS} "$SECRET_ENV_FILE" ${SERVER_USER}@${SERVER_HOST}:${DEPLOY_DIR}/.env
              ssh ${SSH_OPTS} ${SERVER_USER}@${SERVER_HOST} "chmod 600 ${DEPLOY_DIR}/.env"
              echo "Environment file successfully secured with permission 600."
            '''
          }
        }
      }
    }

    stage('Build Container Image') {
      steps {
        sshagent([env.SSH_CREDENTIAL]) {
          sh '''
            echo "Building Podman container image on Homeserver..."
            ssh ${SSH_OPTS} ${SERVER_USER}@${SERVER_HOST} "
              cd ${DEPLOY_DIR}
              podman build -t ${IMAGE_NAME}:latest -f Dockerfile .
            "
            echo "Container image ${IMAGE_NAME}:latest successfully built."
          '''
        }
      }
    }

    stage('Run Podman Container') {
      steps {
        sshagent([env.SSH_CREDENTIAL]) {
          sh '''
            echo "Starting container ${CONTAINER_NAME} on Homeserver..."
            ssh ${SSH_OPTS} ${SERVER_USER}@${SERVER_HOST} "
              echo 'Stopping existing container if running...'
              podman stop ${CONTAINER_NAME} 2>/dev/null || true
              podman rm -f ${CONTAINER_NAME} 2>/dev/null || true

              echo 'Launching new container instance...'
              podman run -d \
                --name ${CONTAINER_NAME} \
                --restart always \
                --network host \
                --env-file ${DEPLOY_DIR}/.env \
                -e PORT='${APP_PORT}' \
                ${IMAGE_NAME}:latest

              echo 'Container launched successfully.'
              podman ps --filter name=${CONTAINER_NAME}
            "
          '''
        }
      }
    }

    stage('Health Check') {
      steps {
        sshagent([env.SSH_CREDENTIAL]) {
          sh '''
            echo "Validating application health endpoint http://127.0.0.1:${APP_PORT}${HEALTH_ENDPOINT}..."
            ssh ${SSH_OPTS} ${SERVER_USER}@${SERVER_HOST} "
              HEALTH_URL='http://127.0.0.1:${APP_PORT}${HEALTH_ENDPOINT}'
              SUCCESS=0
              for i in \$(seq 1 30); do
                HTTP_CODE=\$(curl -s -o /dev/null -w '%{http_code}' \$HEALTH_URL || echo '000')
                if [ \"\$HTTP_CODE\" = '200' ] || [ \"\$HTTP_CODE\" = '204' ]; then
                  echo \"Application is healthy! (HTTP \$HTTP_CODE) [Attempt \$i/30]\"
                  SUCCESS=1
                  break
                fi
                echo \"Waiting for application to become healthy... (HTTP \$HTTP_CODE) [Attempt \$i/30]\"
                sleep 2
              done

              if [ \$SUCCESS -ne 1 ]; then
                echo \"Health check failed after 30 attempts!\" >&2
                echo \"=== Last 50 Container Logs ===\" >&2
                podman logs --tail 50 ${CONTAINER_NAME} >&2
                exit 1
              fi
            "
          '''
        }
      }
    }
  }

  post {
    always {
      archiveArtifacts artifacts: 'coverage.out, .scannerwork/**', allowEmptyArchive: true
    }
    success {
      echo "Pipeline completed successfully! Application is running on port ${APP_PORT}."
    }
    failure {
      echo "Pipeline execution failed. Please check stage logs for details."
    }
  }
}
