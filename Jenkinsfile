pipeline {
    agent any

    options {
        timestamps()
        disableConcurrentBuilds()
    }

    environment {
        REGISTRY = 'ghcr.io'
        IMAGE_NAME = 'portfolio-api'
    }

    stages {
        stage('Validate') {
            steps {
                sh 'go test ./...'
            }
        }

        stage('Build and push image') {
            when {
                branch 'main'
            }
            steps {
                script {
                    if (!env.GHCR_OWNER?.trim()) {
                        error('Set the GHCR_OWNER Jenkins environment variable to your GitHub username or organization.')
                    }
                    def owner = env.GHCR_OWNER.toLowerCase()
                    def tag = env.BUILD_NUMBER
                    withCredentials([usernamePassword(credentialsId: 'ghcr-creds', usernameVariable: 'GHCR_USER', passwordVariable: 'GHCR_TOKEN')]) {
                        sh """
                            set -eu
                            printf '%s' "\$GHCR_TOKEN" | podman login ${REGISTRY} --username "\$GHCR_USER" --password-stdin
                            podman build --pull=always -f Containerfile -t ${REGISTRY}/${owner}/${IMAGE_NAME}:${tag} -t ${REGISTRY}/${owner}/${IMAGE_NAME}:latest .
                            podman push ${REGISTRY}/${owner}/${IMAGE_NAME}:${tag}
                            podman push ${REGISTRY}/${owner}/${IMAGE_NAME}:latest
                            podman logout ${REGISTRY}
                        """
                    }
                }
            }
        }
    }

    post {
        always {
            sh 'podman logout ghcr.io >/dev/null 2>&1 || true'
        }
    }
}
