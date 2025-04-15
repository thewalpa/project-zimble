const startArea = document.getElementById('start-area');
const gameArea = document.getElementById('game-area');
const startGameBtn = document.getElementById('start-game-btn');
const gameIdDisplay = document.getElementById('game-id');
const playerDisplay = document.getElementById('player-display');
const questionArea = document.getElementById('question-area');
const questionText = document.getElementById('question-text');
const answerInput = document.getElementById('answer-input');
const submitAnswerBtn = document.getElementById('submit-answer-btn');
const waitingArea = document.getElementById('waiting-area');
const finishedArea = document.getElementById('finished-area');
const finalScoresDisplay = document.getElementById('final-scores');
const feedback = document.getElementById('feedback');

let currentGameId = null;
let currentPlayerId = null; // Assume we are player 1 for simplicity now
let playerIds = []; // Store both player IDs

// --- API Interaction ---

async function fetchApi(endpoint, options = {}) {
    try {
        const response = await fetch(`/api${endpoint}`, options);
        if (!response.ok) {
            const errorData = await response.json();
            throw new Error(errorData.error || `HTTP error! status: ${response.status}`);
        }
        // Handle cases where the response might be empty (e.g., 204 No Content)
        const contentType = response.headers.get("content-type");
        if (contentType && contentType.indexOf("application/json") !== -1) {
            return await response.json();
        } else {
            return await response.text(); // Or handle as needed
        }
    } catch (error) {
        console.error("API Fetch Error:", error);
        feedback.textContent = `Error: ${error.message}`;
        feedback.style.color = 'red';
        throw error; // Re-throw to allow callers to handle
    }
}

async function createGame() {
    try {
        feedback.textContent = 'Starting game...';
        feedback.style.color = 'black';
        const gameData = await fetchApi('/games', { method: 'POST' });
        console.log("Game created:", gameData);
        currentGameId = gameData.id;
        playerIds = Object.keys(gameData.players);
        // *** SIMPLIFICATION: Assume this browser client controls the FIRST player listed ***
        currentPlayerId = playerIds[0];
        console.log("Controlling Player ID:", currentPlayerId);

        gameIdDisplay.textContent = currentGameId;
        updatePlayerDisplay(gameData.players);
        startArea.classList.add('hidden');
        gameArea.classList.remove('hidden');
        waitingArea.classList.add('hidden');
        finishedArea.classList.add('hidden');
        await loadQuestion();
    } catch (error) {
        // Error already logged by fetchApi
    }
}

async function loadQuestion() {
    if (!currentGameId) return;
    try {
        feedback.textContent = 'Loading question...';
        feedback.style.color = 'black';
        const questionData = await fetchApi(`/games/${currentGameId}/question`);
        console.log("Question received:", questionData);

        if (questionData.message) { // e.g., "No more questions"
            feedback.textContent = questionData.message;
            questionArea.classList.add('hidden');
            await checkGameStatus(); // See if game finished
        } else {
            questionText.textContent = `Q${questionData.index + 1}: ${questionData.text}`;
            answerInput.value = '';
            questionArea.classList.remove('hidden');
            waitingArea.classList.add('hidden');
            finishedArea.classList.add('hidden');
            answerInput.focus();
        }
    } catch (error) {
        // Potentially game ended or error occurred
        if (error.message.includes("not in progress") || error.message.includes("finished")) {
            await checkGameStatus();
        }
    }
}

async function submitAnswer() {
    if (!currentGameId || !currentPlayerId) return;

    const answer = answerInput.value.trim();
    if (!answer) {
        feedback.textContent = 'Please enter an answer.';
        feedback.style.color = 'orange';
        return;
    }

    try {
        feedback.textContent = 'Submitting...';
        feedback.style.color = 'black';
        questionArea.classList.add('hidden'); // Hide input while submitting
        waitingArea.classList.remove('hidden');

        const payload = {
            playerId: currentPlayerId,
            answer: answer
        };

        const resultData = await fetchApi(`/games/${currentGameId}/answer`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });

        console.log("Answer result:", resultData);
        feedback.textContent = `Result: ${resultData.result}. Correct Answer: ${resultData.correctAnswer}`;
        feedback.style.color = resultData.result === 'Correct' ? 'green' : 'red';

        // Update score display immediately (fetch full state to be sure)
        await fetchAndUpdateGameState();

        // Load next question or check status
        if (resultData.gameStatus !== 'finished') {
            await loadQuestion();
        } else {
            await checkGameStatus(); // Explicitly check and display finished state
        }


    } catch (error) {
        // Error already logged by fetchApi
        waitingArea.classList.add('hidden');
        // Maybe show the question area again if submission failed?
        // questionArea.classList.remove('hidden');
    }
}

async function fetchAndUpdateGameState() {
    if (!currentGameId) return;
    try {
        const gameState = await fetchApi(`/games/${currentGameId}`);
        updatePlayerDisplay(gameState.players);
        if (gameState.status === 'finished') {
            displayFinishedState(gameState.players);
        }
        return gameState.status;
    } catch (error) {
        console.error("Failed to fetch game state:", error);
        return null; // Indicate failure
    }
}

async function checkGameStatus() {
    const status = await fetchAndUpdateGameState();
    if (status === 'finished') {
        console.log("Game is finished.");
        // Display already handled by fetchAndUpdateGameState calling displayFinishedState
    } else if (status === 'inprogress') {
        // Maybe we got here because there were no more questions but the server
        // didn't explicitly mark it finished yet. Try loading question again or wait.
        console.log("Game in progress, checking for question again.");
        waitingArea.classList.remove('hidden');
        questionArea.classList.add('hidden');
        finishedArea.classList.add('hidden');
        // Potentially add a delay before retrying loadQuestion
        setTimeout(loadQuestion, 1000); // Wait 1 sec
    } else {
        // Waiting state or error
        waitingArea.classList.remove('hidden');
        questionArea.classList.add('hidden');
        finishedArea.classList.add('hidden');
    }
}


function updatePlayerDisplay(players) {
    playerDisplay.innerHTML = ''; // Clear previous
    for (const playerId in players) {
        const player = players[playerId];
        const playerDiv = document.createElement('div');
        playerDiv.classList.add('player-info');
        playerDiv.textContent = `${player.name}: ${player.score} points`;
        if (playerId === currentPlayerId) {
            playerDiv.style.fontWeight = 'bold'; // Highlight current player
            playerDiv.textContent += " (You)";
        }
        playerDisplay.appendChild(playerDiv);
    }
}

function displayFinishedState(players) {
    questionArea.classList.add('hidden');
    waitingArea.classList.add('hidden');
    finishedArea.classList.remove('hidden');
    let scoresText = "Final Scores: ";
    for (const playerId in players) {
        scoresText += `${players[playerId].name}: ${players[playerId].score} | `;
    }
    finalScoresDisplay.textContent = scoresText.slice(0, -3); // Remove trailing ' | '
    feedback.textContent = "Game Over!";
    feedback.style.color = 'blue';
}


// --- Event Listeners ---
startGameBtn.addEventListener('click', createGame);
submitAnswerBtn.addEventListener('click', submitAnswer);
// Allow submitting with Enter key
answerInput.addEventListener('keypress', (event) => {
    if (event.key === 'Enter') {
        submitAnswer();
    }
});

// --- Initial State ---
gameArea.classList.add('hidden');
startArea.classList.remove('hidden');